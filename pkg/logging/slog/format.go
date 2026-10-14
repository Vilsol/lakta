package slog

import (
	"log/slog"
	"math"
	"time"

	"github.com/lmittmann/tint"
	"github.com/samber/oops"
)

// builtinHandler returns the handler selected by Format, or nil when Format is
// empty (the DI-provided handler is used instead).
func (c *Config) builtinHandler() (slog.Handler, error) {
	var handler slog.Handler

	switch c.Format {
	case "":
		return nil, nil
	case FormatJSON:
		handler = slog.NewJSONHandler(c.Writer, &slog.HandlerOptions{
			AddSource: true,
			// Filtering happens in levelFilter; the handler accepts everything.
			Level: slog.Level(math.MinInt),
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 && a.Key == slog.MessageKey {
					a.Key = "message"
				}
				return a
			},
		})
	case FormatText:
		handler = tint.NewTextHandler(c.Writer, &tint.Options{
			AddSource:  true,
			Level:      slog.Level(math.MinInt),
			TimeFormat: time.RFC3339,
		})
	default:
		return nil, oops.Errorf("unknown log format %q (want json or text)", c.Format)
	}

	var attrs []slog.Attr
	if c.Service != "" {
		attrs = append(attrs, slog.String("service", c.Service))
	}
	if c.Version != "" {
		attrs = append(attrs, slog.String("version", c.Version))
	}

	return handler.WithAttrs(attrs), nil
}
