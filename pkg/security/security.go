// Package security emits security events: one structured WARN log line per
// rejected request (unknown peer, bad token, missing permission), tagged
// log.type=security_event so log pipelines can route them to a SIEM.
//
// Request-scoped fields (request ID, caller SPIFFE ID, client IP) come from the
// ctx logger, which the transport enriches; emitters add what only they know.
package security

import (
	"context"
	"log/slog"

	"github.com/Vilsol/slox"
)

// Event describes one rejection. Realm, Subject and Session must only be set
// from a verified token; leave them empty for rejected tokens.
type Event struct {
	Reason    string
	Procedure string
	Realm     string
	Subject   string
	Session   string
	Attrs     []slog.Attr
}

// Emit logs e at WARN on the ctx logger. Empty fields are omitted.
func Emit(ctx context.Context, e Event) {
	attrs := make([]any, 0, 6+len(e.Attrs)) //nolint:mnd // fixed fields
	attrs = append(attrs, slog.String("log.type", "security_event"), slog.String("reason", e.Reason))
	for _, kv := range [][2]string{{"procedure", e.Procedure}, {"realm", e.Realm}, {"sub", e.Subject}, {"sid", e.Session}} {
		if kv[1] != "" {
			attrs = append(attrs, slog.String(kv[0], kv[1]))
		}
	}
	for _, a := range e.Attrs {
		attrs = append(attrs, a)
	}

	slox.Warn(ctx, "security event", attrs...)
}
