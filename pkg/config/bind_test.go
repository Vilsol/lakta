package config_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
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

func TestBind_BasicGetReturnsConfig(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"app": map[string]any{
			"limits": map[string]any{
				"max_requests": 100,
				"name":         "default",
			},
		},
	})

	mod := config.Bind[testConfig]("app", "limits")
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	cfg := config.Get[testConfig](h.Ctx())
	testza.AssertEqual(t, 100, cfg.MaxRequests)
	testza.AssertEqual(t, "default", cfg.Name)
}

func TestBind_ValidationHappyPath(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"limits": map[string]any{
			"max_requests": 50,
		},
	})

	mod := config.Bind[validatedConfig]("limits")
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	cfg := config.Get[validatedConfig](h.Ctx())
	testza.AssertEqual(t, 50, cfg.MaxRequests)
}

func TestBind_ValidationRejection(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"limits": map[string]any{
			"max_requests": 0,
		},
	})

	mod := config.Bind[validatedConfig]("limits")
	err := mod.Init(h.Ctx())
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "validation failed")
}

func TestBind_HotReloadTriggersOnChange(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"app": map[string]any{
			"limits": map[string]any{
				"max_requests": 100,
				"name":         "initial",
			},
		},
	})

	mod := config.Bind[testConfig]("app", "limits")
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	var callbackValue atomic.Pointer[testConfig]
	binding := config.GetBinding[testConfig](h.Ctx())
	binding.OnChange(func(cfg *testConfig) {
		callbackValue.Store(cfg)
	})

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		"app": map[string]any{
			"limits": map[string]any{
				"max_requests": 200,
				"name":         "updated",
			},
		},
	}), nil))

	h.Notifier().FireReload(newK)

	got := callbackValue.Load()
	testza.AssertNotNil(t, got)
	testza.AssertEqual(t, 200, got.MaxRequests)
	testza.AssertEqual(t, "updated", got.Name)
}

func TestBind_GetReturnsUpdatedValueAfterReload(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"svc": map[string]any{
			"max_requests": 10,
			"name":         "v1",
		},
	})

	mod := config.Bind[testConfig]("svc")
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 10, config.Get[testConfig](h.Ctx()).MaxRequests)

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		"svc": map[string]any{
			"max_requests": 999,
			"name":         "v2",
		},
	}), nil))

	h.Notifier().FireReload(newK)

	testza.AssertEqual(t, 999, config.Get[testConfig](h.Ctx()).MaxRequests)
	testza.AssertEqual(t, "v2", config.Get[testConfig](h.Ctx()).Name)
}

func TestBind_ValidationFailureOnReloadPreservesOldValue(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"limits": map[string]any{
			"max_requests": 50,
		},
	})

	mod := config.Bind[validatedConfig]("limits")
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 50, config.Get[validatedConfig](h.Ctx()).MaxRequests)

	// Reload with invalid config (max_requests = 0)
	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		"limits": map[string]any{
			"max_requests": 0,
		},
	}), nil))

	h.Notifier().FireReload(newK)

	// Old value should be preserved
	testza.AssertEqual(t, 50, config.Get[validatedConfig](h.Ctx()).MaxRequests)
}

func TestBind_Shutdown(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{})
	m := config.Bind[testConfig]("foo")
	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestBind_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "app.limits", config.Bind[testConfig]("app", "limits").ConfigPath())
	testza.AssertEqual(t, "svc", config.Bind[testConfig]("svc").ConfigPath())
}

func TestBind_LoadConfig(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"svc": map[string]any{"max_requests": 10, "name": "v1"},
	})

	m := config.Bind[testConfig]("svc")
	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertEqual(t, 10, config.Get[testConfig](h.Ctx()).MaxRequests)

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		"svc": map[string]any{"max_requests": 99, "name": "v2"},
	}), nil))

	testza.AssertNil(t, m.LoadConfig(newK))
	testza.AssertEqual(t, 99, config.Get[testConfig](h.Ctx()).MaxRequests)
}
