package slog

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/samber/do/v2"
)

//nolint:paralleltest // Module.Init mutates the global slog default; serial by design
func TestLevelControllerSetLevelLetsDebugPass(t *testing.T) {
	upstream := &recordingHandler{}
	h := testkit.NewHarness(t)
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	m := NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "before", pcs[0])
	testza.AssertNil(t, m.filter.Handle(context.Background(), rec))
	testza.AssertEqual(t, 0, len(upstream.records))

	m.SetLevel(slog.LevelDebug)
	testza.AssertEqual(t, slog.LevelDebug, m.Level())

	rec2 := slog.NewRecord(time.Now(), slog.LevelDebug, "after", pcs[0])
	testza.AssertNil(t, m.filter.Handle(context.Background(), rec2))
	testza.AssertEqual(t, 1, len(upstream.records))
}

//nolint:paralleltest // Module.Init mutates the global slog default; serial by design
func TestLevelControllerPreservesPerPackageRules(t *testing.T) {
	upstream := &recordingHandler{}
	h := testkit.NewHarness(t)
	h = testkit.WithProvider(h, func(_ do.Injector) (slog.Handler, error) {
		return upstream, nil
	})

	pkg := laktaModule + "/pkg/logging/slog"
	m := NewModule(WithLevel(levelError), WithLevels(map[string]string{pkg: levelDebug}))
	testza.AssertNil(t, m.Init(h.Ctx()))

	// Change the default level; the per-package debug override must survive.
	m.SetLevel(slog.LevelWarn)
	testza.AssertEqual(t, slog.LevelWarn, m.Level())

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "pkg debug", pcs[0])
	testza.AssertNil(t, m.filter.Handle(context.Background(), rec))
	testza.AssertEqual(t, 1, len(upstream.records))
}
