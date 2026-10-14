package verifier

import (
	"fmt"
	"log/slog"
)

const redacted = "[REDACTED]"

// RawToken is a bearer token kept for forwarding to downstream services. It
// redacts itself in every print path (fmt verbs, slog, JSON); Reveal returns
// the value for the Authorization header.
type RawToken string

// Reveal returns the token itself.
func (t RawToken) Reveal() string { return string(t) }

// String implements fmt.Stringer.
func (t RawToken) String() string { return redacted }

// GoString implements fmt.GoStringer.
func (t RawToken) GoString() string { return redacted }

// Format implements fmt.Formatter so %v, %+v, %#v, %s and %q all redact.
func (t RawToken) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// LogValue implements slog.LogValuer.
func (t RawToken) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON implements json.Marshaler.
func (t RawToken) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
