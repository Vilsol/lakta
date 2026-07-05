package actuator

import (
	"testing"

	"github.com/MarvinJWendt/testza"
)

func newTestRedactor(t *testing.T, additive []string, showValues string) *Redactor {
	t.Helper()
	r, err := NewRedactor(additive, showValues)
	testza.AssertNoError(t, err)
	return r
}

func TestRedactMatchedTopLevelKey(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{"password": "hunter2", "host": "localhost"}, false) //nolint:goconst

	testza.AssertEqual(t, redactMask, out["password"])
	testza.AssertEqual(t, "localhost", out["host"])
}

func TestRedactNestedMapUnderMatchedKey(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{
		"credentials": map[string]any{
			"user": "admin",
			"pass": "s3cret",
		},
	}, false)

	nested, ok := out["credentials"].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, redactMask, nested["user"])
	testza.AssertEqual(t, redactMask, nested["pass"])
}

func TestRedactSliceElements(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{
		"tokens": []any{
			map[string]any{"value": "abc"},
			"postgres://u:p4ss@host/db",
		},
		"hosts": []any{"a", "b"},
	}, false)

	// Slice under a matched key ("tokens" matches "token") is wholesale masked.
	tokens, ok := out["tokens"].([]any)
	testza.AssertTrue(t, ok)
	first, ok := tokens[0].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, redactMask, first["value"])
	testza.AssertEqual(t, redactMask, tokens[1])

	// Slice under an innocuous key is preserved.
	hosts, ok := out["hosts"].([]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "a", hosts[0])
	testza.AssertEqual(t, "b", hosts[1])
}

func TestRedactValueLevelURI(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{"url": "postgres://user:s3cret@host:5432/db"}, false) //nolint:gosec

	testza.AssertEqual(t, "postgres://user:"+redactMask+"@host:5432/db", out["url"])
}

func TestRedactValueLevelDSNParam(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{"extra": "host=db password=abc sslmode=disable"}, false)

	testza.AssertEqual(t, "host=db password="+redactMask+" sslmode=disable", out["extra"])
}

func TestRedactValueLevelQueryString(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{"addr": "https://api/x?password=abc&x=1"}, false)

	testza.AssertEqual(t, "https://api/x?password="+redactMask+"&x=1", out["addr"])
}

func TestRedactRemainSubtreeWholesale(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowNever)
	out := r.Redact(map[string]any{
		"raw": map[string]any{
			"innocuous": "value",
			"nested":    map[string]any{"deep": "thing"},
		},
	}, false)

	raw, ok := out["raw"].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, redactMask, raw["innocuous"])
	nested, ok := raw["nested"].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, redactMask, nested["deep"])
}

func TestRedactShowAlwaysPassthrough(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowAlways)
	out := r.Redact(map[string]any{ //nolint:gosec
		"password": "hunter2",
		"url":      "postgres://user:s3cret@host/db",
		"raw":      map[string]any{"k": "v"},
	}, false)

	testza.AssertEqual(t, "hunter2", out["password"])
	testza.AssertEqual(t, "postgres://user:s3cret@host/db", out["url"])
	raw, ok := out["raw"].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "v", raw["k"])
}

func TestRedactWhenAuthorized(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, nil, ShowWhenAuthorized)

	masked := r.Redact(map[string]any{"password": "hunter2"}, false)
	testza.AssertEqual(t, redactMask, masked["password"])

	shown := r.Redact(map[string]any{"password": "hunter2"}, true)
	testza.AssertEqual(t, "hunter2", shown["password"])
}

func TestRedactAdditivePatternsExtend(t *testing.T) {
	t.Parallel()

	r := newTestRedactor(t, []string{"custom_field"}, ShowNever)
	out := r.Redact(map[string]any{
		"custom_field": "sensitive",
		"password":     "hunter2",
		"host":         "localhost",
	}, false)

	testza.AssertEqual(t, redactMask, out["custom_field"])
	testza.AssertEqual(t, redactMask, out["password"])
	testza.AssertEqual(t, "localhost", out["host"])
}
