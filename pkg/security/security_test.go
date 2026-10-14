package security_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/security"
	"github.com/Vilsol/slox"
)

func TestEmit_LogsSecurityEventWithNonEmptyFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx := slox.Into(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))

	security.Emit(ctx, security.Event{
		Reason:    "missing",
		Procedure: "/errs.tmplsvc.v1.NoteService/GetNote",
		Attrs:     []slog.Attr{slog.String("required_role", "tmplsvc:read")},
	})

	var line map[string]any
	testza.AssertNil(t, json.Unmarshal(buf.Bytes(), &line))
	testza.AssertEqual(t, "WARN", line["level"])
	testza.AssertEqual(t, "security_event", line["log.type"])
	testza.AssertEqual(t, "missing", line["reason"])
	testza.AssertEqual(t, "/errs.tmplsvc.v1.NoteService/GetNote", line["procedure"])
	testza.AssertEqual(t, "tmplsvc:read", line["required_role"])
	_, hasSub := line["sub"]
	testza.AssertFalse(t, hasSub, "unverified identity must not be logged")
}

func TestEmit_IncludesVerifiedIdentity(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx := slox.Into(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))

	security.Emit(ctx, security.Event{Reason: "role", Realm: "qms-internal", Subject: "u-1", Session: "s-1"})

	var line map[string]any
	testza.AssertNil(t, json.Unmarshal(buf.Bytes(), &line))
	testza.AssertEqual(t, "qms-internal", line["realm"])
	testza.AssertEqual(t, "u-1", line["sub"])
	testza.AssertEqual(t, "s-1", line["sid"])
}
