package pgx

import (
	"context"
	"log/slog"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/slox"
	"github.com/jackc/pgx/v5/tracelog"
)

// captureHandler records every slog.Record it receives, capturing all levels.
type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func TestPGXLogger_LevelMapping(t *testing.T) {
	t.Parallel()

	const traceLevel = slog.LevelDebug - 1

	cases := []struct {
		name     string
		in       tracelog.LogLevel
		wantLvl  slog.Level
		wantAttr bool // PGX_LOG_LEVEL attribute present
	}{
		{lvlTrace, tracelog.LogLevelTrace, traceLevel, true},
		{lvlDebug, tracelog.LogLevelDebug, slog.LevelDebug, false},
		{lvlInfo, tracelog.LogLevelInfo, slog.LevelInfo, false},
		{lvlWarn, tracelog.LogLevelWarn, slog.LevelWarn, false},
		{lvlError, tracelog.LogLevelError, slog.LevelError, false},
		{"none-default", tracelog.LogLevelNone, slog.LevelError, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := &captureHandler{}
			ctx := slox.Into(context.Background(), slog.New(h))

			newLogger().Log(ctx, c.in, "msg", map[string]any{"k": "v"})

			testza.AssertEqual(t, 1, len(h.records))
			rec := h.records[0]
			testza.AssertEqual(t, c.wantLvl, rec.Level)
			testza.AssertEqual(t, "msg", rec.Message)

			var hasPGXLevel, hasData bool
			rec.Attrs(func(a slog.Attr) bool {
				switch a.Key {
				case "PGX_LOG_LEVEL":
					hasPGXLevel = true
				case "k":
					hasData = true
				}
				return true
			})
			testza.AssertEqual(t, c.wantAttr, hasPGXLevel)
			testza.AssertTrue(t, hasData)
		})
	}
}
