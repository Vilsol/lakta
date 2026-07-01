package flags_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/features/flags"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

const prefix = "modules.features.flags.default.flags."

const hello = "hello"

func loadKoanf(t *testing.T, data map[string]any) *koanf.Koanf {
	t.Helper()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(data, "."), nil))
	return k
}

func setup(t *testing.T, data map[string]any) (context.Context, *flags.Flags, *flags.Module) {
	t.Helper()
	h := testkit.NewHarness(t)
	m := flags.NewModule()
	testza.AssertNoError(t, m.LoadConfig(loadKoanf(t, data)))
	testza.AssertNoError(t, m.Init(h.Ctx()))

	f, err := do.Invoke[*flags.Flags](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	return h.Ctx(), f, m
}

func TestFlags_BoolReadsScalarValue(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{prefix + "new_checkout": true})

	testza.AssertTrue(t, f.Bool(ctx, "new_checkout", false))
}

func TestFlags_MissingFlagReturnsDefault(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{})

	testza.AssertTrue(t, f.Bool(ctx, "missing", true))
	testza.AssertEqual(t, "fallback", f.String(ctx, "missing", "fallback"))
	testza.AssertEqual(t, 9, f.Int(ctx, "missing", 9))
	testza.AssertEqual(t, 0.5, f.Float(ctx, "missing", 0.5))
	testza.AssertEqual(t, time.Minute, f.Duration(ctx, "missing", time.Minute))
}

func TestFlags_TypedGetters(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{
		prefix + "banner_text": hello,
		prefix + "max_retries": 7,
		prefix + "sample_rate": 0.25,
		prefix + "cache_ttl":   "5m",
	})

	testza.AssertEqual(t, hello, f.String(ctx, "banner_text", ""))
	testza.AssertEqual(t, 7, f.Int(ctx, "max_retries", 0))
	testza.AssertEqual(t, 0.25, f.Float(ctx, "sample_rate", 0))
	testza.AssertEqual(t, 5*time.Minute, f.Duration(ctx, "cache_ttl", 0))
}

func TestFlags_CoercesStringValuesFromEnvOverrides(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{
		prefix + "enabled":     "true",
		prefix + "max_retries": "3",
		prefix + "sample_rate": "0.75",
	})

	testza.AssertTrue(t, f.Bool(ctx, "enabled", false))
	testza.AssertEqual(t, 3, f.Int(ctx, "max_retries", 0))
	testza.AssertEqual(t, 0.75, f.Float(ctx, "sample_rate", 0))
}

func TestFlags_TypeMismatchReturnsDefault(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{prefix + "banner_text": hello})

	testza.AssertFalse(t, f.Bool(ctx, "banner_text", false))
	testza.AssertEqual(t, 4, f.Int(ctx, "banner_text", 4))
}

func TestFlags_BoolReadsObjectFormIgnoringRollout(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{
		prefix + "new_checkout.value":   true,
		prefix + "new_checkout.rollout": 25,
	})

	testza.AssertTrue(t, f.Bool(ctx, "new_checkout", false))
}

func TestFlags_BoolForRolloutBoundaries(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{
		prefix + "all.value":    true,
		prefix + "all.rollout":  100,
		prefix + "none.value":   true,
		prefix + "none.rollout": 0,
		prefix + "off.value":    false,
		prefix + "off.rollout":  100,
		prefix + "plain.value":  true,
	})

	for _, key := range []string{"alice", "bob", "carol"} {
		testza.AssertTrue(t, f.BoolFor(ctx, "all", key, false))
		testza.AssertFalse(t, f.BoolFor(ctx, "none", key, true))
		testza.AssertFalse(t, f.BoolFor(ctx, "off", key, true))
		testza.AssertTrue(t, f.BoolFor(ctx, "plain", key, false))
	}
}

func TestFlags_BoolForIsDeterministicAndRoughlyDistributed(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{
		prefix + "gradual.value":   true,
		prefix + "gradual.rollout": 30,
	})

	enabled := 0
	for i := range 1000 {
		key := fmt.Sprintf("user-%d", i)
		first := f.BoolFor(ctx, "gradual", key, false)
		testza.AssertEqual(t, first, f.BoolFor(ctx, "gradual", key, false))
		if first {
			enabled++
		}
	}

	testza.AssertTrue(t, enabled > 200, "expected >200 enabled, got %d", enabled)
	testza.AssertTrue(t, enabled < 400, "expected <400 enabled, got %d", enabled)
}

func TestFlags_RolloutsAreDecorrelatedAcrossFlags(t *testing.T) {
	t.Parallel()
	ctx, f, _ := setup(t, map[string]any{
		prefix + "exp_a.value":   true,
		prefix + "exp_a.rollout": 50,
		prefix + "exp_b.value":   true,
		prefix + "exp_b.rollout": 50,
	})

	same := 0
	for i := range 1000 {
		key := fmt.Sprintf("user-%d", i)
		if f.BoolFor(ctx, "exp_a", key, false) == f.BoolFor(ctx, "exp_b", key, false) {
			same++
		}
	}

	// Identical hashing across flags would make same == 1000.
	testza.AssertTrue(t, same < 700, "flags appear correlated: %d/1000 matched", same)
}

func TestModule_OnReloadSwapsValues(t *testing.T) {
	t.Parallel()
	ctx, f, m := setup(t, map[string]any{prefix + "banner_text": hello})

	m.OnReload(loadKoanf(t, map[string]any{prefix + "banner_text": "bye"}))

	testza.AssertEqual(t, "bye", f.String(ctx, "banner_text", ""))
}

func TestModule_OnReloadParseFailureKeepsOldSnapshot(t *testing.T) {
	t.Parallel()
	ctx, f, m := setup(t, map[string]any{prefix + "banner_text": hello})

	m.OnReload(loadKoanf(t, map[string]any{
		prefix + "banner_text": "bye",
		prefix + "bad.value":   true,
		prefix + "bad.rollout": 150,
	}))

	testza.AssertEqual(t, hello, f.String(ctx, "banner_text", ""))
}

func TestModule_InitFailsOnInvalidRollout(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := flags.NewModule()
	testza.AssertNoError(t, m.LoadConfig(loadKoanf(t, map[string]any{
		prefix + "bad.value":   true,
		prefix + "bad.rollout": 101,
	})))

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

func TestModule_InitFailsOnObjectWithoutValue(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := flags.NewModule()
	testza.AssertNoError(t, m.LoadConfig(loadKoanf(t, map[string]any{
		prefix + "bad.rollout": 50,
	})))

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

func TestModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.features.flags.default", flags.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.features.flags.custom", flags.NewModule(flags.WithName("custom")).ConfigPath())
}

func TestModule_ProvidesDeclaresFlagsType(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := flags.NewModule()
	testza.AssertNoError(t, m.Init(h.Ctx()))

	f, err := do.Invoke[*flags.Flags](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	testza.AssertNotNil(t, f)
}
