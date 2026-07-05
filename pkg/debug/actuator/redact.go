package actuator

import (
	"regexp"
	"strings"

	"github.com/samber/oops"
)

// defaultKeyInner is the alternation of key substrings whose leaf values are
// treated as secrets. Wrapped in a case-insensitive group by NewRedactor.
const defaultKeyInner = `password|secret|token|key|credential|apikey|dsn|conn.*string`

// redactMask is the replacement for redacted values.
const redactMask = "******"

// Value-level scrubbers catch secrets embedded in string values under innocuous
// keys (url, addr): URI userinfo and DSN/query password-style parameters.
var (
	uriUserinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/?#@\s]+:)([^@\s]+)(@)`)
	dsnParamPattern    = regexp.MustCompile(`(?i)((?:password|secret|token)=)([^&\s;]+)`)
)

// Redactor walks koanf-produced structures redacting secrets. Constructed via
// [Config.SecretRedactor]/[NewRedactor]. Stateless after construction; safe for
// concurrent reads. Redaction is best-effort: value-level scrubbing is
// regex-based and cannot guarantee every embedded secret is caught — the
// wholesale redaction of passthrough (,remain) subtrees is the backstop.
type Redactor struct {
	keyRe      *regexp.Regexp
	showValues string
}

// NewRedactor compiles the default key patterns plus any additive patterns into
// a Redactor honoring showValues (never|always|when_authorized).
func NewRedactor(additive []string, showValues string) (*Redactor, error) {
	var b strings.Builder
	b.WriteString(`(?i)(`)
	b.WriteString(defaultKeyInner)
	for _, p := range additive {
		if p == "" {
			continue
		}
		b.WriteString(`|(?:`)
		b.WriteString(p)
		b.WriteString(`)`)
	}
	b.WriteString(`)`)

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, oops.Wrapf(err, "failed to compile redaction patterns")
	}

	if showValues == "" {
		showValues = ShowNever
	}

	return &Redactor{keyRe: re, showValues: showValues}, nil
}

// masking reports whether values must be masked for this request, honoring
// show_values: never always masks, always never masks, when_authorized masks
// unless the caller is authorized.
func (r *Redactor) masking(authorized bool) bool {
	switch r.showValues {
	case ShowAlways:
		return false
	case ShowWhenAuthorized:
		return !authorized
	default:
		return true
	}
}

// Redact returns a redacted deep copy of a koanf structure. Recurses nested
// map[string]any AND []any elements, wholesale-redacts subtrees under matched
// or passthrough keys, and scrubs value-level URI/DSN secrets in every string.
// When show_values disables masking, returns an unmodified deep copy.
func (r *Redactor) Redact(in map[string]any, authorized bool) map[string]any {
	if !r.masking(authorized) {
		return deepCopyMap(in)
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = r.walk(v, r.keyRe.MatchString(k), isPassthroughKey(k))
	}
	return out
}

// walk redacts a value node. When keyMatched or wholesale is set, the entire
// subtree is masked; otherwise recursion continues and string leaves are
// value-scrubbed.
func (r *Redactor) walk(v any, keyMatched, wholesale bool) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, sub := range val {
			childMatched := keyMatched || wholesale || r.keyRe.MatchString(k)
			out[k] = r.walk(sub, childMatched, wholesale || isPassthroughKey(k))
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, sub := range val {
			out[i] = r.walk(sub, keyMatched, wholesale)
		}
		return out
	case string:
		if keyMatched || wholesale {
			return redactMask
		}
		return r.redactValue(val)
	default:
		if keyMatched || wholesale {
			return redactMask
		}
		return val
	}
}

// redactValue applies the value-level scrubber (URI userinfo, DSN/query params)
// to a single string regardless of key.
func (r *Redactor) redactValue(s string) string {
	s = uriUserinfoPattern.ReplaceAllString(s, `${1}`+redactMask+`${3}`)
	s = dsnParamPattern.ReplaceAllString(s, `${1}`+redactMask)
	return s
}

// isPassthroughKey reports whether a key names a config.Passthrough (,remain)
// subtree, redacted wholesale because its shape is unknown.
func isPassthroughKey(key string) bool {
	return strings.EqualFold(key, "raw")
}

func deepCopyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		out := make([]any, len(val))
		for i, sub := range val {
			out[i] = deepCopyValue(sub)
		}
		return out
	default:
		return val
	}
}
