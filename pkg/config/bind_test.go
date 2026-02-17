package config_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

type testConfig struct {
	MaxRequests int    `koanf:"max_requests"`
	Name        string `koanf:"name"`
}

type validatedConfig struct {
	MaxRequests int `koanf:"max_requests"`
}

func (c *validatedConfig) Validate() error {
	if c.MaxRequests <= 0 {
		return errors.New("max_requests must be positive")
	}
	return nil
}

func setupContext(t *testing.T, data map[string]any) (context.Context, *fakeReloadNotifier) {
	t.Helper()

	injector := do.New()
	ctx := lakta.WithInjector(context.Background(), injector)

	k := koanf.New(".")
	if err := k.Load(mapProvider(data), nil); err != nil {
		t.Fatal(err)
	}

	do.Provide(injector, func(_ do.Injector) (*koanf.Koanf, error) {
		return k, nil
	})

	notifier := &fakeReloadNotifier{}
	do.Provide(injector, func(_ do.Injector) (config.ReloadNotifier, error) {
		return notifier, nil
	})

	return ctx, notifier
}

type fakeReloadNotifier struct {
	callbacks []func(k *koanf.Koanf)
}

func (f *fakeReloadNotifier) OnReload(fn func(k *koanf.Koanf)) {
	f.callbacks = append(f.callbacks, fn)
}

func (f *fakeReloadNotifier) fireReload(k *koanf.Koanf) {
	for _, fn := range f.callbacks {
		fn(k)
	}
}

// mapProvider implements koanf.Provider to load from a map.
type mapProvider map[string]any

func (m mapProvider) ReadBytes() ([]byte, error) { return nil, errors.New("not supported") }
func (m mapProvider) Read() (map[string]any, error) {
	return map[string]any(m), nil
}

func TestBind_BasicGetReturnsConfig(t *testing.T) {
	t.Parallel()

	ctx, _ := setupContext(t, map[string]any{
		"app": map[string]any{
			"limits": map[string]any{
				"max_requests": 100,
				"name":         "default",
			},
		},
	})

	mod := config.Bind[testConfig]("app", "limits")
	err := mod.Init(ctx)
	testza.AssertNil(t, err)

	cfg := config.Get[testConfig](ctx)
	testza.AssertEqual(t, 100, cfg.MaxRequests)
	testza.AssertEqual(t, "default", cfg.Name)
}

func TestBind_ValidationHappyPath(t *testing.T) {
	t.Parallel()

	ctx, _ := setupContext(t, map[string]any{
		"limits": map[string]any{
			"max_requests": 50,
		},
	})

	mod := config.Bind[validatedConfig]("limits")
	err := mod.Init(ctx)
	testza.AssertNil(t, err)

	cfg := config.Get[validatedConfig](ctx)
	testza.AssertEqual(t, 50, cfg.MaxRequests)
}

func TestBind_ValidationRejection(t *testing.T) {
	t.Parallel()

	ctx, _ := setupContext(t, map[string]any{
		"limits": map[string]any{
			"max_requests": 0,
		},
	})

	mod := config.Bind[validatedConfig]("limits")
	err := mod.Init(ctx)
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "validation failed")
}

func TestBind_HotReloadTriggersOnChange(t *testing.T) {
	t.Parallel()

	ctx, notifier := setupContext(t, map[string]any{
		"app": map[string]any{
			"limits": map[string]any{
				"max_requests": 100,
				"name":         "initial",
			},
		},
	})

	mod := config.Bind[testConfig]("app", "limits")
	err := mod.Init(ctx)
	testza.AssertNil(t, err)

	var callbackValue atomic.Pointer[testConfig]
	binding := config.GetBinding[testConfig](ctx)
	binding.OnChange(func(cfg *testConfig) {
		callbackValue.Store(cfg)
	})

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(mapProvider(map[string]any{
		"app": map[string]any{
			"limits": map[string]any{
				"max_requests": 200,
				"name":         "updated",
			},
		},
	}), nil))

	notifier.fireReload(newK)

	got := callbackValue.Load()
	testza.AssertNotNil(t, got)
	testza.AssertEqual(t, 200, got.MaxRequests)
	testza.AssertEqual(t, "updated", got.Name)
}

func TestBind_GetReturnsUpdatedValueAfterReload(t *testing.T) {
	t.Parallel()

	ctx, notifier := setupContext(t, map[string]any{
		"svc": map[string]any{
			"max_requests": 10,
			"name":         "v1",
		},
	})

	mod := config.Bind[testConfig]("svc")
	err := mod.Init(ctx)
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 10, config.Get[testConfig](ctx).MaxRequests)

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(mapProvider(map[string]any{
		"svc": map[string]any{
			"max_requests": 999,
			"name":         "v2",
		},
	}), nil))

	notifier.fireReload(newK)

	testza.AssertEqual(t, 999, config.Get[testConfig](ctx).MaxRequests)
	testza.AssertEqual(t, "v2", config.Get[testConfig](ctx).Name)
}

func TestBind_ValidationFailureOnReloadPreservesOldValue(t *testing.T) {
	t.Parallel()

	ctx, notifier := setupContext(t, map[string]any{
		"limits": map[string]any{
			"max_requests": 50,
		},
	})

	mod := config.Bind[validatedConfig]("limits")
	err := mod.Init(ctx)
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 50, config.Get[validatedConfig](ctx).MaxRequests)

	// Reload with invalid config (max_requests = 0)
	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(mapProvider(map[string]any{
		"limits": map[string]any{
			"max_requests": 0,
		},
	}), nil))

	notifier.fireReload(newK)

	// Old value should be preserved
	testza.AssertEqual(t, 50, config.Get[validatedConfig](ctx).MaxRequests)
}
