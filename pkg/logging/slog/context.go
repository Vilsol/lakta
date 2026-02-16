package slog

import (
	"context"
	"log/slog"
)

type logLevelKey struct{}

// WithLogLevel returns a context that overrides the per-package log level for all
// log records produced within this context. Useful for per-request debug logging.
func WithLogLevel(ctx context.Context, level slog.Level) context.Context {
	return context.WithValue(ctx, logLevelKey{}, level)
}

// LogLevelFromContext returns the log level override from the context, if any.
func LogLevelFromContext(ctx context.Context) (slog.Level, bool) {
	level, ok := ctx.Value(logLevelKey{}).(slog.Level)
	return level, ok
}
