package memory

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collectCounter(t *testing.T, reader *sdkmetric.ManualReader, name, cacheName string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	testza.AssertNil(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			testza.AssertTrue(t, ok)
			var total int64
			for _, dp := range sum.DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key("cache")); ok && v.AsString() == cacheName {
					total += dp.Value
				}
			}
			return total
		}
	}
	return 0
}

func TestStats_HitsMissesToCounterAndOtel(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	b, err := buildOtter("stats", Spec{MaxSize: 1000, RecordStats: true}, mp)
	testza.AssertNoError(t, err)
	t.Cleanup(b.stop)

	load := func(context.Context, any) (any, error) { return 1, nil }

	// First load is a miss; subsequent Gets for the same key are hits.
	_, err = b.GetOrLoad(t.Context(), "a", load)
	testza.AssertNoError(t, err)
	for range 3 {
		_, err = b.GetOrLoad(t.Context(), "a", load)
		testza.AssertNoError(t, err)
	}

	s := b.Stats()
	testza.AssertEqual(t, uint64(3), s.Hits)
	testza.AssertEqual(t, uint64(1), s.Misses)

	testza.AssertEqual(t, int64(3), collectCounter(t, reader, "cache_hits_total", "stats"))
	testza.AssertEqual(t, int64(1), collectCounter(t, reader, "cache_misses_total", "stats"))
}

func TestStats_EvictionsToCounterAndOtel(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	b, err := buildOtter("evict", Spec{MaxSize: 1, RecordStats: true}, mp)
	testza.AssertNoError(t, err)
	t.Cleanup(b.stop)

	for i := range 200 {
		b.Set(i, i)
	}
	b.oc.CleanUp() // force pending maintenance so evictions are accounted

	s := b.Stats()
	testza.AssertGreater(t, s.Evictions, uint64(0))
	testza.AssertGreater(t, collectCounter(t, reader, "cache_evictions_total", "evict"), int64(0))
}

func TestStats_SizeGaugeObserved(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	b, err := buildOtter("sized", Spec{MaxSize: 1000, RecordStats: true}, mp)
	testza.AssertNoError(t, err)
	t.Cleanup(b.stop)

	b.Set("a", 1)
	b.Set("b", 2)

	var rm metricdata.ResourceMetrics
	testza.AssertNil(t, reader.Collect(context.Background(), &rm))
	var found bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "cache_size" {
				found = true
			}
		}
	}
	testza.AssertTrue(t, found)
}
