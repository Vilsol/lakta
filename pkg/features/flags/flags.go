package flags

import (
	"context"
	"hash/fnv"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Vilsol/slox"
	"github.com/samber/oops"
)

const maxRollout = 100

type flagValue struct {
	value   any
	rollout *int
}

type snapshot map[string]flagValue

// Flags exposes lock-free reads over the current flag snapshot. The snapshot
// is swapped wholesale on config hot-reload, so reads never observe a
// half-applied reload.
type Flags struct {
	snap atomic.Pointer[snapshot]
}

func newFlags(s snapshot) *Flags {
	f := &Flags{}
	f.snap.Store(&s)
	return f
}

func (f *Flags) swap(s snapshot) {
	f.snap.Store(&s)
}

func (f *Flags) lookup(name string) (flagValue, bool) {
	v, ok := (*f.snap.Load())[name]
	return v, ok
}

// Bool returns the flag's boolean value, or def if missing or mistyped.
// Rollout percentages are ignored; use [Flags.BoolFor] for rollout-aware reads.
func (f *Flags) Bool(ctx context.Context, name string, def bool) bool {
	return read(ctx, f, name, def, toBool)
}

// BoolFor returns the flag's boolean value gated by its rollout percentage:
// a stable hash of the flag name and key decides whether key is in the
// enabled bucket. Without a rollout it behaves like [Flags.Bool].
func (f *Flags) BoolFor(ctx context.Context, name, key string, def bool) bool {
	fl, ok := f.lookup(name)
	if !ok {
		return def
	}
	val, ok := toBool(fl.value)
	if !ok {
		warnMistyped(ctx, name)
		return def
	}
	if !val || fl.rollout == nil {
		return val
	}
	return bucket(name, key) < *fl.rollout
}

// String returns the flag's string value, or def if missing or mistyped.
func (f *Flags) String(ctx context.Context, name, def string) string {
	return read(ctx, f, name, def, toString)
}

// Int returns the flag's integer value, or def if missing or mistyped.
func (f *Flags) Int(ctx context.Context, name string, def int) int {
	return read(ctx, f, name, def, toInt)
}

// Float returns the flag's float value, or def if missing or mistyped.
func (f *Flags) Float(ctx context.Context, name string, def float64) float64 {
	return read(ctx, f, name, def, toFloat)
}

// Duration returns the flag's duration value, or def if missing or mistyped.
func (f *Flags) Duration(ctx context.Context, name string, def time.Duration) time.Duration {
	return read(ctx, f, name, def, toDuration)
}

//nolint:ireturn // generic accessor returns the coerced value
func read[T any](ctx context.Context, f *Flags, name string, def T, coerce func(any) (T, bool)) T {
	fl, ok := f.lookup(name)
	if !ok {
		return def
	}
	v, ok := coerce(fl.value)
	if !ok {
		warnMistyped(ctx, name)
		return def
	}
	return v
}

func warnMistyped(ctx context.Context, name string) {
	slox.Warn(ctx, "feature flag has unexpected type, using default", slog.String("flag", name))
}

// bucket maps (flag, key) to a stable value in [0, 100). Hashing the flag
// name too decorrelates rollouts: two 50% flags enable different key sets.
func bucket(name, key string) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(key))
	return int(h.Sum64() % maxRollout)
}

// parseSnapshot validates raw flag definitions: scalars pass through, object
// form requires a value field and an optional integer rollout in [0, 100].
func parseSnapshot(raw map[string]any) (snapshot, error) {
	s := make(snapshot, len(raw))
	for name, v := range raw {
		obj, ok := v.(map[string]any)
		if !ok {
			s[name] = flagValue{value: v}
			continue
		}

		val, ok := obj["value"]
		if !ok {
			return nil, oops.Errorf("flag %q: object form requires a value field", name)
		}
		fl := flagValue{value: val}
		if r, ok := obj["rollout"]; ok {
			ri, ok := toInt(r)
			if !ok || ri < 0 || ri > maxRollout {
				return nil, oops.Errorf("flag %q: rollout must be an integer between 0 and 100", name)
			}
			fl.rollout = &ri
		}
		s[name] = fl
	}
	return s, nil
}

// Coercers are lenient with strings because env-var overrides always arrive
// as strings (LAKTA_MODULES__FEATURES__FLAGS__...=true).

func toBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		b, err := strconv.ParseBool(t)
		return b, err == nil
	default:
		return false, false
	}
}

func toString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		if t == float64(int(t)) {
			return int(t), true
		}
		return 0, false
	case string:
		i, err := strconv.Atoi(t)
		return i, err == nil
	default:
		return 0, false
	}
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toDuration(v any) (time.Duration, bool) {
	switch t := v.(type) {
	case time.Duration:
		return t, true
	case string:
		d, err := time.ParseDuration(t)
		return d, err == nil
	default:
		return 0, false
	}
}
