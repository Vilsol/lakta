package slog

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

//nolint:paralleltest // Module.Init mutates the global slog default; serial by design
func TestFormatJSON_OnbKeysServiceVersionAndTrace(t *testing.T) {
	var buf bytes.Buffer
	h := testkit.NewHarness(t)
	m := NewModule(WithFormat(FormatJSON), WithWriter(&buf), WithService("tmplsvc"), WithVersion("1.2.3"))
	testza.AssertNil(t, m.Init(h.Ctx()))

	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(t.Context(), "op")
	defer span.End()
	sc := span.SpanContext()
	m.logger.InfoContext(ctx, "hello", "k", "v")

	var line map[string]any
	testza.AssertNil(t, json.Unmarshal(buf.Bytes(), &line))
	testza.AssertEqual(t, "hello", line["message"])
	testza.AssertEqual(t, "INFO", line["level"])
	testza.AssertEqual(t, "tmplsvc", line["service"])
	testza.AssertEqual(t, "1.2.3", line["version"])
	testza.AssertEqual(t, sc.TraceID().String(), line["trace_id"])
	testza.AssertNotNil(t, line["time"])
	_, hasMsg := line["msg"]
	testza.AssertFalse(t, hasMsg)
}

//nolint:paralleltest // Module.Init mutates the global slog default; serial by design
func TestFormatText_UsesTint(t *testing.T) {
	var buf bytes.Buffer
	h := testkit.NewHarness(t)
	m := NewModule(WithFormat(FormatText), WithWriter(&buf))
	testza.AssertNil(t, m.Init(h.Ctx()))

	m.logger.Info("hello")
	testza.AssertTrue(t, strings.Contains(buf.String(), "INF"))
	testza.AssertFalse(t, strings.HasPrefix(buf.String(), "{"))
}

//nolint:paralleltest // Module.Init mutates the global slog default; serial by design
func TestFormatUnset_RequiresHandlerInDI(t *testing.T) {
	h := testkit.NewHarness(t)
	m := NewModule()
	testza.AssertNotNil(t, m.Init(h.Ctx()))
}
