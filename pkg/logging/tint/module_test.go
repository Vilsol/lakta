package tint_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/logging/tint"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/samber/do/v2"
)

func TestTintModule_ProvidesHandler(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{})
	m := tint.NewModule()

	testza.AssertNil(t, m.Init(h.Ctx()))

	handler, err := do.Invoke[slog.Handler](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, handler)
}

func TestTintModule_InitWithWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := testkit.NewHarness(t)
	m := tint.NewModule(tint.WithWriter(&buf))

	testza.AssertNil(t, m.Init(h.Ctx()))

	handler, err := do.Invoke[slog.Handler](h.Injector())
	testza.AssertNil(t, err)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	testza.AssertNil(t, handler.Handle(context.Background(), record))
	testza.AssertTrue(t, buf.Len() > 0)
}

func TestTintModule_ConfigPathDefault(t *testing.T) {
	t.Parallel()

	m := tint.NewModule()
	testza.AssertEqual(t, "modules.logging.tint.default", m.ConfigPath())
}

func TestTintModule_ConfigPathNamed(t *testing.T) {
	t.Parallel()

	m := tint.NewModule(tint.WithName("custom"))
	testza.AssertEqual(t, "modules.logging.tint.custom", m.ConfigPath())
}

func TestTintModule_ShutdownNoop(t *testing.T) {
	t.Parallel()

	m := tint.NewModule()
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestTintModule_NoKoanfSucceeds(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t) // no koanf in DI
	m := tint.NewModule()

	testza.AssertNil(t, m.Init(h.Ctx()))
}

func TestTintModule_Name_Default(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", tint.NewModule().Name())
}

func TestTintModule_Name_Custom(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "custom", tint.NewModule(tint.WithName("custom")).Name())
}
