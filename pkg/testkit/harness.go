package testkit

import (
	"context"
	"errors"
	"testing"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

// Harness provides a test context with a DI injector for testing lakta modules.
type Harness struct {
	t        *testing.T
	injector do.Injector
	ctx      context.Context //nolint:containedctx
	notifier *ReloadNotifier
}

// NewHarness creates a new test harness with an injector-backed context.
func NewHarness(t *testing.T) *Harness {
	t.Helper()
	injector := do.New()
	ctx := lakta.WithInjector(context.Background(), injector)

	return &Harness{
		t:        t,
		injector: injector,
		ctx:      ctx,
		notifier: &ReloadNotifier{},
	}
}

// WithData loads a map into koanf and provides *koanf.Koanf and config.ReloadNotifier in DI.
func (h *Harness) WithData(data map[string]any) *Harness {
	h.t.Helper()
	k := koanf.New(".")
	if err := k.Load(MapProvider(data), nil); err != nil {
		h.t.Fatal(err)
	}
	return h.WithKoanf(k)
}

// WithKoanf provides a pre-built koanf and config.ReloadNotifier in DI.
func (h *Harness) WithKoanf(k *koanf.Koanf) *Harness {
	do.Provide(h.injector, func(_ do.Injector) (*koanf.Koanf, error) {
		return k, nil
	})
	do.Provide(h.injector, func(_ do.Injector) (config.ReloadNotifier, error) {
		return h.notifier, nil
	})
	return h
}

// Ctx returns the context with the injector embedded.
func (h *Harness) Ctx() context.Context {
	return h.ctx
}

// Notifier returns the shared reload notifier.
func (h *Harness) Notifier() *ReloadNotifier {
	return h.notifier
}

// Injector returns the DI injector.
func (h *Harness) Injector() do.Injector { //nolint:ireturn
	return h.injector
}

// WithProvider registers an arbitrary DI provider in the harness.
func WithProvider[T any](h *Harness, provider func(do.Injector) (T, error)) *Harness {
	do.Provide(h.injector, provider)
	return h
}

// MapProvider implements koanf.Provider for loading configuration from a map.
type MapProvider map[string]any

func (m MapProvider) ReadBytes() ([]byte, error)    { return nil, errors.New("not supported") }
func (m MapProvider) Read() (map[string]any, error) { return map[string]any(m), nil }

// ReloadNotifier is a test double for config.ReloadNotifier.
type ReloadNotifier struct {
	callbacks []func(*koanf.Koanf)
}

// OnReload implements config.ReloadNotifier.
func (r *ReloadNotifier) OnReload(fn func(*koanf.Koanf)) {
	r.callbacks = append(r.callbacks, fn)
}

// FireReload invokes all registered callbacks with the given koanf instance.
func (r *ReloadNotifier) FireReload(k *koanf.Koanf) {
	for _, fn := range r.callbacks {
		fn(k)
	}
}
