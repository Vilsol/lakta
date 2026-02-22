package slog

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

var (
	_ lakta.Module        = (*Module)(nil)
	_ lakta.Provider      = (*Module)(nil)
	_ lakta.Dependent     = (*Module)(nil)
	_ lakta.HotReloadable = (*Module)(nil)
)

func withRecordingHandler(h *testkit.Harness) *testkit.Harness {
	rh := &recordingHandler{}
	return testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return rh, nil
	})
}

func TestSlogModule_NewModuleDoesNotHang(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	go func() {
		_ = NewModule()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NewModule() did not return within 1 second")
	}
}

func TestSlogModule_InitProvidesLogger(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	h = withRecordingHandler(h)

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))

	logger, err := do.Invoke[*slog.Logger](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, logger)
}

func TestSlogModule_InitWithoutHandlerFails(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t) // no slog.Handler in DI
	m := NewModule()

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

func TestSlogModule_InitWithoutKoanfSucceeds(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t) // no koanf in DI
	h = withRecordingHandler(h)

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))
}

func TestSlogModule_DefaultLevelFiltersDebug(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	h := testkit.NewHarness(t)
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))

	// Default level is info; debug records must not reach the upstream handler.
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "debug msg", pcs[0])
	testza.AssertNil(t, m.filter.Handle(context.Background(), rec))
	testza.AssertEqual(t, 0, len(upstream.records))
}

func TestSlogModule_ConfigLevelFromKoanf(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	h := testkit.NewHarness(t).WithData(map[string]any{
		"logging": map[string]any{
			"level": "debug",
		},
	})
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "debug msg", pcs[0])
	testza.AssertNil(t, m.filter.Handle(context.Background(), rec))
	testza.AssertEqual(t, 1, len(upstream.records))
}

func TestSlogModule_ReloadUpdatesLevel(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	h := testkit.NewHarness(t).WithData(map[string]any{
		"logging": map[string]any{"level": "info"},
	})
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))

	// Reload with level=error; after this, info records should be dropped.
	newK := koanf.New(".")
	testza.AssertNil(t, newK.Load(testkit.MapProvider(map[string]any{
		"logging": map[string]any{"level": "error"},
	}), nil))
	m.OnReload(newK)

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "info msg", pcs[0])
	testza.AssertNil(t, m.filter.Handle(context.Background(), rec))
	testza.AssertEqual(t, 0, len(upstream.records))
}

func TestSlogModule_ShutdownNoop(t *testing.T) {
	t.Parallel()

	m := NewModule()
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestSlogModule_ValidateLevelPrefixes_KnownPrefix(t *testing.T) {
	t.Parallel()

	// A known module prefix (this module itself) — must not warn or fail.
	upstream := &recordingHandler{}
	h := testkit.NewHarness(t).WithData(map[string]any{
		"logging": map[string]any{
			"level": "info",
			"levels": map[string]any{
				"github.com/Vilsol/lakta": "debug",
			},
		},
	})
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))
}

func TestSlogModule_ValidateLevelPrefixes_UnknownPrefix(t *testing.T) {
	t.Parallel()

	// An unknown prefix — Init still succeeds, only a warning is logged.
	upstream := &recordingHandler{}
	h := testkit.NewHarness(t).WithData(map[string]any{
		"logging": map[string]any{
			"level": "info",
			"levels": map[string]any{
				"github.com/nonexistent/totally/unknown/pkg": "debug",
			},
		},
	})
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))
}
