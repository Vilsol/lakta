package slog

import (
	"log/slog"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
)

func TestParseLevel_AllVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  slog.Level
	}{
		{levelDebug, slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{levelInfo, slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{levelWarn, slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{levelError, slog.LevelError},
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
		"pkg/foo": levelWarn,
		"pkg/bar": levelError,
		"pkg/baz": levelDebug,
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
		WithLevel(levelWarn),
		WithLevels(map[string]string{"pkg/x": levelDebug}),
	)

	testza.AssertEqual(t, "mylogger", c.Name)
	testza.AssertEqual(t, levelWarn, c.Level)
	testza.AssertEqual(t, levelDebug, c.Levels["pkg/x"])
}

func TestConfig_NewDefaultConfig(t *testing.T) {
	t.Parallel()

	c := NewDefaultConfig()
	testza.AssertEqual(t, config.DefaultInstanceName, c.Name)
	testza.AssertEqual(t, levelInfo, c.Level)
	testza.AssertTrue(t, c.GlobalDefault)
}
