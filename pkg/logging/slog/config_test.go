package slog

import (
	"log/slog"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func TestParseLevel_AllVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelDebug},
		{"", slog.LevelDebug},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			testza.AssertEqual(t, tc.want, parseLevel(tc.input))
		})
	}
}

func TestConfig_ParseLevels_NonEmpty(t *testing.T) {
	t.Parallel()

	c := NewDefaultConfig()
	c.Levels = map[string]string{
		"pkg/foo": "warn",
		"pkg/bar": "error",
		"pkg/baz": "debug",
	}
	c.ParseLevels()

	testza.AssertEqual(t, slog.LevelWarn, c.levelsParsed["pkg/foo"])
	testza.AssertEqual(t, slog.LevelError, c.levelsParsed["pkg/bar"])
	testza.AssertEqual(t, slog.LevelDebug, c.levelsParsed["pkg/baz"])
}

func TestConfig_ParseLevels_Empty(t *testing.T) {
	t.Parallel()

	c := NewDefaultConfig()
	c.ParseLevels() // no-op when Levels is empty
	testza.AssertNil(t, c.levelsParsed)
}

func TestConfig_WithOptions(t *testing.T) {
	t.Parallel()

	c := NewConfig(
		WithName("mylogger"),
		WithLevel("warn"),
		WithLevels(map[string]string{"pkg/x": "debug"}),
	)

	testza.AssertEqual(t, "mylogger", c.Name)
	testza.AssertEqual(t, "warn", c.Level)
	testza.AssertEqual(t, "debug", c.Levels["pkg/x"])
}

func TestConfig_NewDefaultConfig(t *testing.T) {
	t.Parallel()

	c := NewDefaultConfig()
	testza.AssertEqual(t, "default", c.Name)
	testza.AssertEqual(t, "info", c.Level)
	testza.AssertTrue(t, c.GlobalDefault)
}
