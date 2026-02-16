package slog

import (
	"context"
	"log/slog"
	"runtime"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func TestExtractPackagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"method on pointer receiver", "github.com/org/repo/pkg.(*Type).Method", "github.com/org/repo/pkg"},
		{"plain function", "github.com/org/repo/pkg.Function", "github.com/org/repo/pkg"},
		{"nested package", "github.com/org/repo/pkg/sub/deep.Function", "github.com/org/repo/pkg/sub/deep"},
		{"main package", "main.main", "main"},
		{"empty string", "", ""},
		{"no dots", "github.com/org/repo/pkg", "github.com/org/repo/pkg"},
		{"closure", "github.com/org/repo/pkg.Function.func1", "github.com/org/repo/pkg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := extractPackagePath(tt.input)
			testza.AssertEqual(t, tt.expected, result)
		})
	}
}

func TestMatchLevel(t *testing.T) {
	t.Parallel()

	s := &levelRules{
		defaultLevel: slog.LevelInfo,
		rules: []levelRule{
			{prefix: "github.com/org/repo/pkg/grpc/server", level: slog.LevelWarn},
			{prefix: "github.com/org/repo/pkg/grpc", level: slog.LevelDebug},
			{prefix: "github.com/org/repo/pkg/db", level: slog.LevelError},
		},
	}

	tests := []struct {
		name     string
		pkgPath  string
		expected slog.Level
	}{
		{"exact match longest", "github.com/org/repo/pkg/grpc/server", slog.LevelWarn},
		{"sub-package of longest", "github.com/org/repo/pkg/grpc/server/internal", slog.LevelWarn},
		{"shorter prefix match", "github.com/org/repo/pkg/grpc/client", slog.LevelDebug},
		{"different subtree", "github.com/org/repo/pkg/db", slog.LevelError},
		{"no match falls to default", "github.com/org/repo/pkg/http", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := matchLevel(s, tt.pkgPath)
			testza.AssertEqual(t, tt.expected, result)
		})
	}
}

func TestLevelFilter_Enabled(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	f := newLevelFilter(upstream, slog.LevelWarn, map[string]slog.Level{
		"github.com/org/repo/pkg": slog.LevelDebug,
	})

	// minLevel is Debug (from the override), so Debug should be enabled
	testza.AssertTrue(t, f.Enabled(context.Background(), slog.LevelDebug))
	testza.AssertTrue(t, f.Enabled(context.Background(), slog.LevelWarn))
	testza.AssertTrue(t, f.Enabled(context.Background(), slog.LevelError))
}

func TestLevelFilter_EnabledWithContextOverride(t *testing.T) {
	t.Parallel()

	upstream := &recordingHandler{}
	f := newLevelFilter(upstream, slog.LevelError, nil)

	// Without context override, Debug is below minLevel (Error)
	testza.AssertFalse(t, f.Enabled(context.Background(), slog.LevelDebug))

	// With context override to Debug, Debug should be enabled
	ctx := WithLogLevel(context.Background(), slog.LevelDebug)
	testza.AssertTrue(t, f.Enabled(ctx, slog.LevelDebug))
	testza.AssertTrue(t, f.Enabled(ctx, slog.LevelError))
}

func TestLevelFilter_Handle(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelWarn, map[string]slog.Level{
		"github.com/Vilsol/lakta/pkg/logging/slog": slog.LevelDebug,
	})

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	record := slog.Record{}
	record.Level = slog.LevelDebug
	record.Message = "test debug"
	record.PC = pcs[0]

	err := f.Handle(context.Background(), record)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 1, len(handler.records))
	testza.AssertEqual(t, "test debug", handler.records[0].Message)
}

func TestLevelFilter_HandleDropsBelowThreshold(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelError, map[string]slog.Level{
		"github.com/some/other/pkg": slog.LevelDebug,
	})

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	record := slog.Record{}
	record.Level = slog.LevelInfo
	record.Message = "should be dropped"
	record.PC = pcs[0]

	err := f.Handle(context.Background(), record)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 0, len(handler.records))
}

func TestLevelFilter_HandleWithContextOverride(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelError, nil)

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	// Debug would normally be dropped (default is Error), but context overrides to Debug
	ctx := WithLogLevel(context.Background(), slog.LevelDebug)

	record := slog.Record{}
	record.Level = slog.LevelDebug
	record.Message = "context override"
	record.PC = pcs[0]

	err := f.Handle(ctx, record)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 1, len(handler.records))
	testza.AssertEqual(t, "context override", handler.records[0].Message)
}

func TestLevelFilter_HandleContextOverrideDropsBelowOverride(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelDebug, nil)

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	// Context overrides to Warn — Debug should be dropped even though default allows it
	ctx := WithLogLevel(context.Background(), slog.LevelWarn)

	record := slog.Record{}
	record.Level = slog.LevelDebug
	record.Message = "should be dropped by context"
	record.PC = pcs[0]

	err := f.Handle(ctx, record)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 0, len(handler.records))
}

func TestLevelFilter_Update(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelError, nil)

	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	// Before update: Debug is dropped (default is Error)
	record := slog.Record{}
	record.Level = slog.LevelDebug
	record.Message = "before update"
	record.PC = pcs[0]

	err := f.Handle(context.Background(), record)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 0, len(handler.records))

	// After update: add override for this package to Debug
	f.Update(slog.LevelError, map[string]slog.Level{
		"github.com/Vilsol/lakta/pkg/logging/slog": slog.LevelDebug,
	})

	record2 := slog.Record{}
	record2.Level = slog.LevelDebug
	record2.Message = "after update"
	record2.PC = pcs[0]

	err = f.Handle(context.Background(), record2)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 1, len(handler.records))
	testza.AssertEqual(t, "after update", handler.records[0].Message)
}

func TestLevelFilter_WithAttrs(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelInfo, map[string]slog.Level{
		"pkg": slog.LevelDebug,
	})

	wrapped := f.WithAttrs([]slog.Attr{slog.String("key", "val")})
	_, ok := wrapped.(*levelFilter)
	testza.AssertTrue(t, ok)
}

func TestLevelFilter_WithGroup(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	f := newLevelFilter(handler, slog.LevelInfo, map[string]slog.Level{
		"pkg": slog.LevelDebug,
	})

	wrapped := f.WithGroup("grp")
	_, ok := wrapped.(*levelFilter)
	testza.AssertTrue(t, ok)
}

func TestWithLogLevel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// No override initially
	_, ok := LogLevelFromContext(ctx)
	testza.AssertFalse(t, ok)

	// Set override
	ctx = WithLogLevel(ctx, slog.LevelWarn)
	level, ok := LogLevelFromContext(ctx)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, slog.LevelWarn, level)
}

func TestPrefixMatchesAnyModule(t *testing.T) {
	t.Parallel()

	modules := []string{
		"github.com/Vilsol/lakta",
		"github.com/gofiber/fiber/v2",
	}

	tests := []struct {
		name     string
		prefix   string
		expected bool
	}{
		{"exact module match", "github.com/Vilsol/lakta", true},
		{"sub-package of module", "github.com/Vilsol/lakta/pkg/grpc", true},
		{"module is prefix of configured prefix", "github.com/gofiber/fiber/v2/middleware", true},
		{"no match", "github.com/unknown/pkg", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := prefixMatchesAnyModule(tt.prefix, modules)
			testza.AssertEqual(t, tt.expected, result)
		})
	}
}

type recordingHandler struct {
	records []slog.Record
	attrs   []slog.Attr
	group   string
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record)
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recordingHandler{attrs: attrs}
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	return &recordingHandler{group: name}
}
