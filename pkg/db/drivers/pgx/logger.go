package pgx

import (
	"context"
	"log/slog"

	"github.com/Vilsol/slox"
	"github.com/jackc/pgx/v5/tracelog"
)

var _ tracelog.Logger = (*pgxLogger)(nil)

type pgxLogger struct{}

func newLogger() *pgxLogger {
	return &pgxLogger{}
}

func (l *pgxLogger) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]any) {
	attrs := make([]slog.Attr, 0, len(data))
	for k, v := range data {
		attrs = append(attrs, slog.Any(k, v))
	}

	var lvl slog.Level
	switch level {
	case tracelog.LogLevelTrace:
		lvl = slog.LevelDebug - 1
		attrs = append(attrs, slog.Any("PGX_LOG_LEVEL", level))
	case tracelog.LogLevelDebug:
		lvl = slog.LevelDebug
	case tracelog.LogLevelInfo:
		lvl = slog.LevelInfo
	case tracelog.LogLevelWarn:
		lvl = slog.LevelWarn
	case tracelog.LogLevelError:
		lvl = slog.LevelError
	default:
		lvl = slog.LevelError
	}

	slox.LogAttrs(ctx, lvl, msg, attrs...)
}
