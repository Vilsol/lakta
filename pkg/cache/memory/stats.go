package memory

import (
	"context"

	"github.com/maypok86/otter/v2/stats" // verified v2.3.0
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// meterName identifies this module's meter.
const meterName = "github.com/Vilsol/lakta/pkg/cache/memory"

// statsRecorder implements otter's stats.Recorder, pushing into otel
// instruments created off the DI MeterProvider. Off a noop meter the
// instruments no-op, so no otel-present branch is needed.
//
// It embeds a *stats.Counter (which implements both stats.Recorder and
// stats.Snapshoter): otter's Cache.Stats() reads a zero snapshot unless the
// recorder is a Snapshoter, so the embedded counter keeps Cache.Stats()
// populated while the overridden Record* methods also fan out to otel.
type statsRecorder struct {
	*stats.Counter

	name      string
	meter     otelmetric.Meter
	attrs     otelmetric.MeasurementOption
	hits      otelmetric.Int64Counter
	misses    otelmetric.Int64Counter
	evictions otelmetric.Int64Counter
	size      otelmetric.Int64ObservableGauge
}

// newStatsRecorder builds the recorder (embedding a fresh stats.Counter) plus
// otel instruments off the provided MeterProvider. Instrument construction
// errors yield usable no-op instruments, so they are ignored.
func newStatsRecorder(name string, mp otelmetric.MeterProvider) *statsRecorder {
	meter := mp.Meter(meterName)
	hits, _ := meter.Int64Counter("cache_hits_total")
	misses, _ := meter.Int64Counter("cache_misses_total")
	evictions, _ := meter.Int64Counter("cache_evictions_total")
	size, _ := meter.Int64ObservableGauge("cache_size")

	return &statsRecorder{
		Counter:   stats.NewCounter(),
		name:      name,
		meter:     meter,
		attrs:     otelmetric.WithAttributes(attribute.String("cache", name)),
		hits:      hits,
		misses:    misses,
		evictions: evictions,
		size:      size,
	}
}

// observeSize registers a callback reporting the cache's live size on the
// cache_size gauge. The returned Registration is unregistered on rebuild/stop.
func (r *statsRecorder) observeSize(sizeFn func() int64) (otelmetric.Registration, error) { //nolint:ireturn // Registration is the library return type
	//nolint:wrapcheck // Registration/err are returned to the caller for Unregister
	return r.meter.RegisterCallback(func(_ context.Context, o otelmetric.Observer) error {
		o.ObserveInt64(r.size, sizeFn(), r.attrs)
		return nil
	}, r.size)
}

func (r *statsRecorder) RecordHits(count int) {
	r.Counter.RecordHits(count)
	r.hits.Add(context.Background(), int64(count), r.attrs)
}

func (r *statsRecorder) RecordMisses(count int) {
	r.Counter.RecordMisses(count)
	r.misses.Add(context.Background(), int64(count), r.attrs)
}

func (r *statsRecorder) RecordEviction(weight uint32) {
	r.Counter.RecordEviction(weight)
	r.evictions.Add(context.Background(), 1, r.attrs)
}
