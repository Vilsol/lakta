package pgx

import (
	"context"
	"errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestQueryName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"sqlc many", "-- name: GetUsers :many\nSELECT * FROM users", "GetUsers"},
		{"sqlc one", "-- name: GetUserByID :one\nSELECT 1 WHERE id = $1", "GetUserByID"},
		{"no comment", "SELECT version()", "SELECT"},
		{"leading blank lines", "\n\nINSERT INTO t VALUES (1)", "INSERT"},
		{"other comment then sql", "-- license header\nUPDATE t SET x = 1", "UPDATE"},
		{"empty", "", "unnamed"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			testza.AssertEqual(t, c.want, queryName(c.sql))
		})
	}
}

func collectHistogram(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	testza.AssertNil(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			testza.AssertTrue(t, ok)
			testza.AssertEqual(t, 1, len(h.DataPoints))
			return h.DataPoints[0]
		}
	}
	t.Fatalf("histogram %q not found", name)
	return metricdata.HistogramDataPoint[float64]{}
}

func TestErrorType_PgErrorSQLSTATE(t *testing.T) {
	t.Parallel()

	err := &pgconn.PgError{Code: "23505"}
	testza.AssertEqual(t, "23505", errorType(err))
}

func TestErrorType_NonPgError(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "_OTHER", errorType(errors.New("boom")))
}

func TestTraceQueryEnd_NoStartValue_NoOp(t *testing.T) {
	t.Parallel()

	// ctx lacks the start value -> early return, must not panic with a nil
	// duration instrument.
	tr := &queryMetricsTracer{}
	tr.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
}

//nolint:paralleltest // mutates the global otel MeterProvider; serial by design
func TestQueryMetricsTracer_RecordsSQLSTATE(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	tr := newQueryMetricsTracer("testdb")
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL: "-- name: InsertUser :exec\nINSERT INTO users VALUES (1)",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "23505"}})

	dp := collectHistogram(t, reader, "db.client.query.duration")
	et, ok := dp.Attributes.Value(attribute.Key("error.type"))
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "23505", et.AsString())
}

//nolint:paralleltest // mutates the global otel MeterProvider; serial by design
func TestQueryMetricsTracer_RecordsSuccess(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	tr := newQueryMetricsTracer("testdb")
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL: "-- name: GetUsers :many\nSELECT 1",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	dp := collectHistogram(t, reader, "db.client.query.duration")
	testza.AssertEqual(t, uint64(1), dp.Count)

	name, ok := dp.Attributes.Value(attribute.Key("db.query.name"))
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "GetUsers", name.AsString())

	ns, ok := dp.Attributes.Value(attribute.Key("db.namespace"))
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "testdb", ns.AsString())

	_, hasErr := dp.Attributes.Value(attribute.Key("error.type"))
	testza.AssertFalse(t, hasErr)
}

//nolint:paralleltest // mutates the global otel MeterProvider; serial by design
func TestQueryMetricsTracer_RecordsErrorType(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	tr := newQueryMetricsTracer("testdb")
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL: "-- name: GetUsers :many\nSELECT 1",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("boom")})

	dp := collectHistogram(t, reader, "db.client.query.duration")
	et, ok := dp.Attributes.Value(attribute.Key("error.type"))
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "_OTHER", et.AsString())
}
