package pgx

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const unnamedQuery = "unnamed"

const meterName = "github.com/Vilsol/lakta/pkg/db/drivers/pgx"

// queryName extracts the sqlc query name from a leading "-- name: X :cmd"
// comment. Falls back to the leading SQL verb (uppercased), then "unnamed".
// It never returns raw SQL — the result is a bounded-cardinality metric label.
func queryName(sql string) string {
	for _, line := range strings.Split(sql, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "-- name:"); ok {
			if fields := strings.Fields(rest); len(fields) >= 1 {
				return fields[0]
			}
			return unnamedQuery
		}
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "/*") {
			continue // skip non-sqlc comments
		}
		if fields := strings.Fields(line); len(fields) >= 1 {
			return strings.ToUpper(fields[0])
		}
	}
	return unnamedQuery
}

// queryMetricsTracer is a pgx.QueryTracer that records query latency as a
// histogram labeled by sqlc query name. It uses a distinct instrument name
// from otelpgx's db.client.operation.duration to avoid double-counting.
type queryMetricsTracer struct {
	duration  metric.Float64Histogram
	namespace attribute.KeyValue
}

type queryMetricsCtxKey struct{}

type queryMetricsCtxValue struct {
	start time.Time
	name  string
}

// newQueryMetricsTracer builds the tracer. database is read once from the pool
// config (constant per pool), so no per-query conn.Config() copy is needed.
func newQueryMetricsTracer(database string) *queryMetricsTracer {
	// On error the OTel API returns a usable no-op instrument, so ignoring the
	// error is safe (matches otelpgx behaviour when metrics are disabled).
	duration, _ := otel.GetMeterProvider().Meter(meterName).Float64Histogram(
		"db.client.query.duration",
		metric.WithDescription("Duration of database queries, by sqlc query name."),
		metric.WithUnit("s"),
	)
	return &queryMetricsTracer{
		duration:  duration,
		namespace: attribute.String("db.namespace", database),
	}
}

func (t *queryMetricsTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryMetricsCtxKey{}, queryMetricsCtxValue{
		start: time.Now(),
		name:  queryName(data.SQL),
	})
}

func (t *queryMetricsTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	val, ok := ctx.Value(queryMetricsCtxKey{}).(queryMetricsCtxValue)
	if !ok {
		return
	}

	attrs := make([]attribute.KeyValue, 0, 3)
	attrs = append(attrs, attribute.String("db.query.name", val.name), t.namespace)
	if data.Err != nil {
		attrs = append(attrs, attribute.String("error.type", errorType(data.Err)))
	}

	t.duration.Record(ctx, time.Since(val.start).Seconds(), metric.WithAttributes(attrs...))
}

// errorType maps a query error to a bounded-cardinality label: the SQLSTATE
// code for Postgres errors, otherwise the semconv "_OTHER".
func errorType(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return "_OTHER"
}
