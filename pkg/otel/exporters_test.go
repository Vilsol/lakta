package otel_test

import (
	"context"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/otel"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/do/v2"
	otelmetric "go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const exporterNone = "none"

func noExporters() map[string]string {
	return map[string]string{"traces": exporterNone, "metrics": exporterNone, "logs": exporterNone}
}

//nolint:paralleltest // setup mutates OTel globals
func TestExportersNone_KeepsRecordingSpansAndPrometheus(t *testing.T) {
	h := testkit.NewHarness(t)
	m := otel.NewModule(otel.WithExporters(noExporters()))
	testza.AssertNil(t, m.Init(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	tp, err := do.Invoke[oteltrace.TracerProvider](lakta.GetInjector(h.Ctx()))
	testza.AssertNil(t, err)
	_, span := tp.Tracer("t").Start(t.Context(), "op")
	testza.AssertTrue(t, span.IsRecording())
	span.End()

	mp, err := do.Invoke[otelmetric.MeterProvider](lakta.GetInjector(h.Ctx()))
	testza.AssertNil(t, err)
	counter, err := mp.Meter("t").Int64Counter("errs_test_total")
	testza.AssertNil(t, err)
	counter.Add(t.Context(), 3)

	reg, err := do.Invoke[*prometheus.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNil(t, err)
	families, err := reg.Gather()
	testza.AssertNil(t, err)

	found := false
	for _, f := range families {
		if f.GetName() == "errs_test_total" {
			found = true
		}
	}
	testza.AssertTrue(t, found, "counter must be exposed on the private registry")
}

//nolint:paralleltest // setup mutates OTel globals
func TestExportersNone_ShutdownDoesNotWaitOnCollector(t *testing.T) {
	h := testkit.NewHarness(t)
	m := otel.NewModule(otel.WithExporters(noExporters()))
	testza.AssertNil(t, m.Init(h.Ctx()))

	start := time.Now()
	testza.AssertNil(t, m.Shutdown(context.Background()))
	testza.AssertTrue(t, time.Since(start) < time.Second)
}

//nolint:paralleltest // setup mutates OTel globals
func TestDisabled_StillProvidesRegistry(t *testing.T) {
	h := testkit.NewHarness(t)
	m := otel.NewModule(otel.WithEnabled(false))
	testza.AssertNil(t, m.Init(h.Ctx()))

	reg, err := do.Invoke[*prometheus.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, reg)
}

//nolint:paralleltest // setup mutates OTel globals
func TestUnknownExporterRejected(t *testing.T) {
	h := testkit.NewHarness(t)
	m := otel.NewModule(otel.WithExporters(map[string]string{"traces": "zipkin"}), otel.WithRequired(true))
	testza.AssertNotNil(t, m.Init(h.Ctx()))
}
