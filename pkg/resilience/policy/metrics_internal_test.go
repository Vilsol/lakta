package policy

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/failsafe-go/failsafe-go"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var rm metricdata.ResourceMetrics
	testza.AssertNoError(t, reader.Collect(context.Background(), &rm))

	out := make(map[string]metricdata.Aggregation)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m.Data
		}
	}
	return out
}

func int64Sum(agg metricdata.Aggregation) int64 {
	sum, ok := agg.(metricdata.Sum[int64])
	if !ok {
		return -1
	}
	var total int64
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	return total
}

func int64Gauge(agg metricdata.Aggregation) int64 {
	g, ok := agg.(metricdata.Gauge[int64])
	if !ok || len(g.DataPoints) == 0 {
		return -1
	}
	return g.DataPoints[0].Value
}

//nolint:paralleltest // mutates the global otel MeterProvider; must not run beside parallel tests
func TestPolicyMetrics_ShedAndGauges(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	pm := newPolicyMetrics()
	testza.AssertNotNil(t, pm)

	pc := PolicyConfig{AdaptiveLimiter: &AdaptiveLimiterConfig{Min: 1, Max: 1, Initial: 1}}
	built, limiter, err := pc.buildWithMetrics("api", pm)
	testza.AssertNoError(t, err)
	testza.AssertNotNil(t, limiter)
	testza.AssertNoError(t, pm.registerLimiterGauges("api", limiter))

	ex := failsafe.With(built...)
	const n = 4
	release := make(chan struct{})
	admitted := make(chan struct{}, 1)
	done := make(chan error, n)
	for range n {
		go func() {
			done <- ex.RunWithExecution(func(_ failsafe.Execution[any]) error {
				admitted <- struct{}{}
				<-release
				return nil
			})
		}()
	}

	<-admitted // one execution holds the single permit
	for range n - 1 {
		<-done // drain the sheds
	}

	metrics := collectMetrics(t, reader)

	shed, ok := metrics["resilience.shed.total"]
	testza.AssertTrue(t, ok, "resilience.shed.total must be recorded")
	testza.AssertTrue(t, int64Sum(shed) >= 1, "shed counter must increment at least once")

	limit, ok := metrics["resilience.limit"]
	testza.AssertTrue(t, ok, "resilience.limit gauge must be registered")
	testza.AssertEqual(t, int64(1), int64Gauge(limit))

	_, ok = metrics["resilience.inflight"]
	testza.AssertTrue(t, ok, "resilience.inflight gauge must be registered")

	close(release)
	<-done // the admitted execution completes
}

func TestPolicyMetrics_NilProviderNoPanic(t *testing.T) {
	t.Parallel()

	pc := PolicyConfig{
		Hedge:           &HedgeConfig{Delay: 10, MaxHedges: 1},
		AdaptiveLimiter: &AdaptiveLimiterConfig{Min: 1, Max: 2, Initial: 1},
		Bulkhead:        &BulkheadConfig{MaxConcurrent: 1},
	}
	built, limiter, err := pc.buildWithMetrics("", nil)
	testza.AssertNoError(t, err)
	testza.AssertNotNil(t, limiter)
	testza.AssertNotNil(t, built)
}
