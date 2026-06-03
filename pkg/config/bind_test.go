package config_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
)

const (
	keyApp         = "app"
	keyLimits      = "limits"
	keyMaxRequests = "max_requests"
	keyDefault     = "default"
	keySvc         = "svc"
	keyName        = "name"
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
		keyApp: map[string]any{
			keyLimits: map[string]any{
				keyMaxRequests: 100,
				keyName:        keyDefault,
			},
		},
	})

	mod := config.Bind[testConfig](keyApp, keyLimits)
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	cfg := config.Get[testConfig](h.Ctx())
	testza.AssertEqual(t, 100, cfg.MaxRequests)
	testza.AssertEqual(t, keyDefault, cfg.Name)
}

func TestBind_ValidationHappyPath(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		keyLimits: map[string]any{
			keyMaxRequests: 50,
		},
	})

	mod := config.Bind[validatedConfig](keyLimits)
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	cfg := config.Get[validatedConfig](h.Ctx())
	testza.AssertEqual(t, 50, cfg.MaxRequests)
}

func TestBind_ValidationRejection(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		keyLimits: map[string]any{
			keyMaxRequests: 0,
		},
	})

	mod := config.Bind[validatedConfig](keyLimits)
	err := mod.Init(h.Ctx())
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "validation failed")
}

func TestBind_HotReloadTriggersOnChange(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		keyApp: map[string]any{
			keyLimits: map[string]any{
				keyMaxRequests: 100,
				keyName:        "initial",
			},
		},
	})

	mod := config.Bind[testConfig](keyApp, keyLimits)
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	var callbackValue atomic.Pointer[testConfig]
	binding := config.GetBinding[testConfig](h.Ctx())
	binding.OnChange(func(cfg *testConfig) {
		callbackValue.Store(cfg)
	})

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		keyApp: map[string]any{
			keyLimits: map[string]any{
				keyMaxRequests: 200,
				keyName:        "updated",
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
		keySvc: map[string]any{
			keyMaxRequests: 10,
			keyName:        "v1",
		},
	})

	mod := config.Bind[testConfig](keySvc)
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 10, config.Get[testConfig](h.Ctx()).MaxRequests)

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		keySvc: map[string]any{
			keyMaxRequests: 999,
			keyName:        "v2",
		},
	}), nil))

	h.Notifier().FireReload(newK)

	testza.AssertEqual(t, 999, config.Get[testConfig](h.Ctx()).MaxRequests)
	testza.AssertEqual(t, "v2", config.Get[testConfig](h.Ctx()).Name)
}

func TestBind_ValidationFailureOnReloadPreservesOldValue(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		keyLimits: map[string]any{
			keyMaxRequests: 50,
		},
	})

	mod := config.Bind[validatedConfig](keyLimits)
	err := mod.Init(h.Ctx())
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 50, config.Get[validatedConfig](h.Ctx()).MaxRequests)

	// Reload with invalid config (max_requests = 0)
	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		keyLimits: map[string]any{
			keyMaxRequests: 0,
		},
	}), nil))

	h.Notifier().FireReload(newK)

	// Old value should be preserved
	testza.AssertEqual(t, 50, config.Get[validatedConfig](h.Ctx()).MaxRequests)
}

//nolint:paralleltest // mutates the global default slog logger; must not run in parallel.
func TestBind_ValidationFailureOnReloadLogsError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := testkit.NewHarness(t).WithData(map[string]any{
		keyLimits: map[string]any{
			keyMaxRequests: 50,
		},
	})

	mod := config.Bind[validatedConfig](keyLimits)
	testza.AssertNil(t, mod.Init(h.Ctx()))
	testza.AssertEqual(t, 50, config.Get[validatedConfig](h.Ctx()).MaxRequests)

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		keyLimits: map[string]any{
			keyMaxRequests: 0,
		},
	}), nil))

	h.Notifier().FireReload(newK)

	// Old value retained.
	testza.AssertEqual(t, 50, config.Get[validatedConfig](h.Ctx()).MaxRequests)

	// Failure is observable: error logged with the config path.
	logged := buf.String()
	testza.AssertContains(t, logged, "ERROR")
	testza.AssertContains(t, logged, "limits")
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

	testza.AssertEqual(t, "app.limits", config.Bind[testConfig](keyApp, keyLimits).ConfigPath())
	testza.AssertEqual(t, "svc", config.Bind[testConfig](keySvc).ConfigPath())
}

func TestBind_LoadConfig(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		keySvc: map[string]any{keyMaxRequests: 10, keyName: "v1"},
	})

	m := config.Bind[testConfig](keySvc)
	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertEqual(t, 10, config.Get[testConfig](h.Ctx()).MaxRequests)

	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		keySvc: map[string]any{keyMaxRequests: 99, keyName: "v2"},
	}), nil))

	testza.AssertNil(t, m.LoadConfig(newK))
	testza.AssertEqual(t, 99, config.Get[testConfig](h.Ctx()).MaxRequests)
}
