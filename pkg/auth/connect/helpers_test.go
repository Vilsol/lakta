package authconnect_test

import (
	"io"
	"log/slog"
)

func slogJSON(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}
