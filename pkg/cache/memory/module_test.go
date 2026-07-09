package memory_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/cache"
	"github.com/Vilsol/lakta/pkg/cache/memory"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	otelmetric "go.opentelemetry.io/otel/metric"
)

const prefix = "modules.cache.memory.default.caches."

func loadKoanf(t *testing.T, data map[string]any) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(data, "."), nil))
	return k
}

func setup(t *testing.T, data map[string]any, options ...memory.Option) (*cache.Registry, *memory.Module, *logSpy) {
	t.Helper()
	reg, m, spy, _ := setupWith(t, data, nil, options...)
	return reg, m, spy
}

func setupWith(
	t *testing.T,
	data map[string]any,
	mp otelmetric.MeterProvider,
	options ...memory.Option,
) (*cache.Registry, *memory.Module, *logSpy, context.Context) {
	t.Helper()
	h := testkit.NewHarness(t)
	if mp != nil {
		testkit.WithProvider(h, func(_ do.Injector) (otelmetric.MeterProvider, error) { return mp, nil })
	}
	spy := newLogSpy()
	ctx := slox.Into(h.Ctx(), slog.New(spy))

	m := memory.NewModule(options...)
	testza.AssertNoError(t, m.LoadConfig(loadKoanf(t, data)))
	testza.AssertNoError(t, m.Init(ctx))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	reg, err := do.Invoke[*cache.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)

	return reg, m, spy, ctx
}

func TestModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.cache.memory.default", memory.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.cache.memory.custom", memory.NewModule(memory.WithName("custom")).ConfigPath())
}

func TestNamed_TypeIdentityGuard(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{prefix + "sessions.max_size": 100})

	c1, err := cache.Named[string, int](reg, "sessions")
	testza.AssertNoError(t, err)

	_, err = cache.Named[string, string](reg, "sessions")
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "string, int")
	testza.AssertContains(t, err.Error(), "string, string")

	c2, err := cache.Named[string, int](reg, "sessions")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, c1, c2)
}

func TestNamed_UnknownDiagnostic(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{prefix + "sessions.max_size": 10})

	_, err := cache.Named[string, int](reg, "missing")
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "not registered")
	testza.AssertContains(t, err.Error(), "sessions")
}

func TestModule_WithCacheCodeOption(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{}, memory.WithCache("code", memory.Spec{MaxSize: 10}))

	c, err := cache.Named[string, int](reg, "code")
	testza.AssertNoError(t, err)
	c.Set("a", 1)
	v, ok := c.Get("a")
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 1, v)
}

func TestGetOrLoad_SingleflightCoalesces(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{prefix + "sf.max_size": 1000})

	c, err := cache.Named[string, int](reg, "sf")
	testza.AssertNoError(t, err)

	const n = 32
	var calls atomic.Int64
	load := func(_ context.Context, _ string) (int, error) { //nolint:unparam // signature must match cache.Loader
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return 7, nil
	}

	var wg sync.WaitGroup
	results := make([]int, n)
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			v, err := c.GetOrLoad(t.Context(), "key", load)
			testza.AssertNoError(t, err)
			results[i] = v
		})
	}
	close(start)
	wg.Wait()

	testza.AssertEqual(t, int64(1), calls.Load())
	for _, v := range results {
		testza.AssertEqual(t, 7, v)
	}
}

func TestTTL_WriteExpiry(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{
		prefix + "ttl.max_size": 100,
		prefix + "ttl.ttl":      "60ms",
	})

	c, err := cache.Named[string, int](reg, "ttl")
	testza.AssertNoError(t, err)

	c.Set("a", 1)
	v, ok := c.Get("a")
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 1, v)

	time.Sleep(120 * time.Millisecond)
	_, ok = c.Get("a")
	testza.AssertFalse(t, ok)
}

func TestTTLAccess_ResetsOnGet(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{
		prefix + "acc.max_size":   100,
		prefix + "acc.ttl_access": "80ms",
	})

	c, err := cache.Named[string, int](reg, "acc")
	testza.AssertNoError(t, err)

	c.Set("a", 1)
	// Keep accessing within the window; access resets the expiry.
	for range 3 {
		time.Sleep(40 * time.Millisecond)
		_, ok := c.Get("a")
		testza.AssertTrue(t, ok)
	}

	time.Sleep(160 * time.Millisecond)
	_, ok := c.Get("a")
	testza.AssertFalse(t, ok)
}

func TestNoMeter_StillWorks(t *testing.T) {
	t.Parallel()
	reg, _, _ := setup(t, map[string]any{
		prefix + "nm.max_size":     100,
		prefix + "nm.record_stats": true,
	})

	c, err := cache.Named[string, int](reg, "nm")
	testza.AssertNoError(t, err)

	v, err := c.GetOrLoad(t.Context(), "a", func(context.Context, string) (int, error) { return 9, nil })
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 9, v)

	v, ok := c.Get("a")
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 9, v)
}

func TestReload_TTLChangeRebuildsAndWarns(t *testing.T) {
	t.Parallel()
	reg, m, spy := setup(t, map[string]any{
		prefix + "r.max_size": 100,
		prefix + "r.ttl":      "1h",
	})

	c, err := cache.Named[string, int](reg, "r")
	testza.AssertNoError(t, err)
	c.Set("a", 1)
	_, ok := c.Get("a")
	testza.AssertTrue(t, ok)

	m.OnReload(loadKoanf(t, map[string]any{
		prefix + "r.max_size": 100,
		prefix + "r.ttl":      "2h",
	}))

	_, ok = c.Get("a")
	testza.AssertFalse(t, ok) // rebuilt -> entries dropped
	testza.AssertTrue(t, spy.contains("dropped"))
}

func TestReload_MaxSizeChangeKeepsEntries(t *testing.T) {
	t.Parallel()
	reg, m, spy := setup(t, map[string]any{prefix + "r.max_size": 100})

	c, err := cache.Named[string, int](reg, "r")
	testza.AssertNoError(t, err)
	c.Set("a", 1)

	m.OnReload(loadKoanf(t, map[string]any{prefix + "r.max_size": 500}))

	v, ok := c.Get("a")
	testza.AssertTrue(t, ok) // live resize -> no entry loss
	testza.AssertEqual(t, 1, v)
	testza.AssertFalse(t, spy.contains("dropped"))
}

func TestShutdown_StopsCaches(t *testing.T) {
	t.Parallel()
	reg, m, _ := setup(t, map[string]any{prefix + "a.max_size": 10, prefix + "b.max_size": 10})

	_, err := cache.Named[string, int](reg, "a")
	testza.AssertNoError(t, err)
	_, err = cache.Named[string, int](reg, "b")
	testza.AssertNoError(t, err)

	testza.AssertNoError(t, m.Shutdown(t.Context()))
}

type logSpy struct {
	mu   sync.Mutex
	msgs []string
}

func newLogSpy() *logSpy { return &logSpy{} }

func (s *logSpy) Enabled(context.Context, slog.Level) bool { return true }

func (s *logSpy) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, r.Message)
	return nil
}

func (s *logSpy) WithAttrs([]slog.Attr) slog.Handler { return s }

func (s *logSpy) WithGroup(string) slog.Handler { return s }

func (s *logSpy) contains(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}
