package memory

import (
	"context"
	"log/slog"
	"maps"
	"reflect"
	"sync"

	"github.com/Vilsol/lakta/pkg/cache"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	"github.com/maypok86/otter/v2" // verified v2.3.0
	"github.com/samber/oops"
	otelmetric "go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

// Module builds otter caches from MergedCaches and provides the core
// *cache.Registry via DI. Plain Module (not long-running) — no Start.
type Module struct {
	lakta.NamedBase

	config    Config
	registry  *cache.Registry
	mp        otelmetric.MeterProvider
	reloadCtx context.Context //nolint:containedctx // captured for reload-time logging, like scheduler

	mu    sync.Mutex
	specs map[string]Spec
	live  map[string]*otterBox
}

// NewModule creates a new in-memory cache module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryCache, "memory", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init resolves the optional DI MeterProvider (noop when otel is absent), then
// registers a Builder per MergedCaches() entry into a fresh *cache.Registry.
// Otter needs concrete [K,V] at build time but config only knows sizing, so each
// Builder defers otter construction (as otter.Cache[any,any]) to the first
// Named[K,V] call. ProvideValue lets app modules Named[K,V] it during their Init.
func (m *Module) Init(ctx context.Context) error {
	m.mp = optionalMeter(ctx)
	m.reloadCtx = context.WithoutCancel(ctx)
	m.registry = cache.NewRegistry()
	m.specs = m.config.MergedCaches()
	m.live = make(map[string]*otterBox)

	for name := range m.specs {
		m.registry.Register(name, m.builder(name))
	}

	lakta.ProvideValue(ctx, m.registry)

	return nil
}

// builder returns a cache.Builder that materializes the named otter cache on
// first use, reading the (possibly reloaded) spec by name.
func (m *Module) builder(name string) cache.Builder {
	return func(_, _ reflect.Type) (any, error) {
		return m.buildBox(name)
	}
}

func (m *Module) buildBox(name string) (*otterBox, error) {
	m.mu.Lock()
	spec := m.specs[name]
	m.mu.Unlock()

	b, err := buildOtter(name, spec, m.mp)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.live[name] = b
	m.mu.Unlock()

	return b, nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*cache.Registry](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
// The otel module always provides a MeterProvider (noop when disabled), so
// declaring it orders otel before this module.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[otelmetric.MeterProvider](),
	}
}

// Shutdown stops every built otter cache's background goroutines.
func (m *Module) Shutdown(_ context.Context) error {
	m.mu.Lock()
	live := make([]*otterBox, 0, len(m.live))
	for _, b := range m.live {
		live = append(live, b)
	}
	m.mu.Unlock()

	for _, b := range live {
		b.stop()
	}

	return nil
}

// OnReload re-loads config then diffs MergedCaches() against the live caches:
// a max_size-only change resizes live (no entry loss, no warning); a ttl/
// ttl_access/record_stats change rebuilds the otter cache and logs a loud
// warning that entries were dropped; added names register a lazy builder;
// removed names stop and drop the cache.
func (m *Module) OnReload(k *koanf.Koanf) {
	ctx := m.reloadCtx

	reloaded := NewDefaultConfig()
	reloaded.Name = m.config.Name
	reloaded.CodeCaches = m.config.CodeCaches

	if err := reloaded.LoadFromKoanf(k, m.ConfigPath()); err != nil {
		slox.Error(ctx, "failed to reload cache config", slog.Any("error", err))
		return
	}

	m.config = reloaded
	desired := reloaded.MergedCaches()

	m.mu.Lock()
	old := m.specs
	m.specs = desired
	live := maps.Clone(m.live)
	m.mu.Unlock()

	for name := range old {
		if _, ok := desired[name]; ok {
			continue
		}
		if b, built := live[name]; built {
			b.stop()
			m.mu.Lock()
			delete(m.live, name)
			m.mu.Unlock()
		}
		m.registry.Unregister(name)
	}

	for name := range desired {
		if _, ok := old[name]; !ok {
			m.registry.Register(name, m.builder(name))
		}
	}

	for name, spec := range desired {
		oldSpec, existed := old[name]
		if !existed || oldSpec == spec {
			continue
		}

		b, built := live[name]
		if !built {
			continue // spec updated in place; the next Named builds with new sizing
		}

		if onlyMaxSizeChanged(oldSpec, spec) && spec.MaxSize > 0 {
			b.setMaximum(spec)
			continue
		}

		if err := b.rebuild(spec, m.mp); err != nil {
			slox.Error(ctx, "failed to rebuild cache on reload", slog.String("cache", name), slog.Any("error", err))
			continue
		}

		slox.Warn(ctx, "cache rebuilt on config reload; all entries were dropped", slog.String("cache", name))
	}
}

// onlyMaxSizeChanged reports whether MaxSize is the sole differing field.
func onlyMaxSizeChanged(a, b Spec) bool {
	return a.MaxSize != b.MaxSize &&
		a.TTL == b.TTL &&
		a.TTLAccess == b.TTLAccess &&
		a.RecordStats == b.RecordStats
}

// optionalMeter resolves a MeterProvider from DI, or a noop provider when otel
// is absent entirely.
func optionalMeter(ctx context.Context) otelmetric.MeterProvider { //nolint:ireturn // MeterProvider is the library interface
	if mp, err := lakta.Invoke[otelmetric.MeterProvider](ctx); err == nil {
		return mp
	}

	return noopmetric.NewMeterProvider()
}

// otterBox wraps *otter.Cache[any,any] behind the core cache seam. Keys/values
// are boxed as any because the concrete (K,V) is unknown at build time; the
// registry's type-identity guard keeps the boxed assertions safe. The RWMutex
// guards oc so a hot-reload rebuild can swap the underlying cache while readers
// keep working through the same handle.
type otterBox struct {
	mu      sync.RWMutex
	oc      *otter.Cache[any, any]
	rec     *statsRecorder
	sizeReg otelmetric.Registration
	spec    Spec
	name    string
}

func (b *otterBox) current() *otter.Cache[any, any] {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.oc
}

func (b *otterBox) Get(key any) (any, bool) {
	return b.current().GetIfPresent(key) // verified v2.3.0
}

func (b *otterBox) Set(key, value any) {
	b.current().Set(key, value) // verified v2.3.0: returns (V, bool), discarded
}

func (b *otterBox) Delete(key any) {
	b.current().Invalidate(key) // verified v2.3.0: returns (V, bool), discarded
}

func (b *otterBox) GetOrLoad(ctx context.Context, key any, load func(context.Context, any) (any, error)) (any, error) {
	//nolint:wrapcheck // propagate the caller's loader error unchanged; verified v2.3.0: built-in singleflight loader
	return b.current().Get(ctx, key, otter.LoaderFunc[any, any](load))
}

func (b *otterBox) Stats() cache.Stats {
	oc := b.current()
	s := oc.Stats()
	return cache.Stats{
		Hits:      s.Hits,
		Misses:    s.Misses,
		Evictions: s.Evictions,
		Size:      oc.EstimatedSize(),
	}
}

// setMaximum applies a live max-size resize with no entry loss.
func (b *otterBox) setMaximum(spec Spec) {
	b.mu.Lock()
	b.spec = spec
	oc := b.oc
	b.mu.Unlock()
	oc.SetMaximum(uint64(spec.MaxSize)) //nolint:gosec // MaxSize is validated > 0 before a live resize; verified v2.3.0
}

// rebuild swaps in a freshly-built otter cache (dropping all entries) for a
// TTL/record_stats change otter cannot mutate live.
func (b *otterBox) rebuild(spec Spec, mp otelmetric.MeterProvider) error {
	fresh, err := buildOtter(b.name, spec, mp)
	if err != nil {
		return err
	}

	b.mu.Lock()
	oldOC, oldReg := b.oc, b.sizeReg
	b.oc, b.rec, b.sizeReg, b.spec = fresh.oc, fresh.rec, fresh.sizeReg, spec
	b.mu.Unlock()

	if oldReg != nil {
		_ = oldReg.Unregister()
	}
	oldOC.StopAllGoroutines()

	return nil
}

// stop unregisters the size gauge and stops the cache's background goroutines.
func (b *otterBox) stop() {
	b.mu.Lock()
	oc, reg := b.oc, b.sizeReg
	b.sizeReg = nil
	b.mu.Unlock()

	if reg != nil {
		_ = reg.Unregister()
	}
	oc.StopAllGoroutines() // verified v2.3.0
}

// buildOtter constructs the otter cache for one Spec, mapping MaxSize ->
// MaximumSize, TTL/TTLAccess -> the single ExpiryCalculator (TTLAccess wins when
// both are set, as it is a superset of write-only expiry), and RecordStats ->
// the otel-fanning statsRecorder. It then registers the size gauge callback.
func buildOtter(name string, spec Spec, mp otelmetric.MeterProvider) (*otterBox, error) {
	opts := otter.Options[any, any]{}
	if spec.MaxSize > 0 {
		opts.MaximumSize = spec.MaxSize
	}

	switch {
	case spec.TTLAccess > 0:
		opts.ExpiryCalculator = otter.ExpiryAccessing[any, any](spec.TTLAccess) // verified v2.3.0
	case spec.TTL > 0:
		opts.ExpiryCalculator = otter.ExpiryWriting[any, any](spec.TTL) // verified v2.3.0
	}

	var rec *statsRecorder
	if spec.RecordStats {
		rec = newStatsRecorder(name, mp)
		opts.StatsRecorder = rec
	}

	oc, err := otter.New(&opts) // verified v2.3.0
	if err != nil {
		return nil, oops.Wrapf(err, "failed to build otter cache %q", name)
	}

	var sizeReg otelmetric.Registration
	if rec != nil {
		sizeReg, err = rec.observeSize(func() int64 { return int64(oc.EstimatedSize()) })
		if err != nil {
			return nil, oops.Wrapf(err, "failed to register cache_size gauge for %q", name)
		}
	}

	return &otterBox{oc: oc, rec: rec, sizeReg: sizeReg, spec: spec, name: name}, nil
}
