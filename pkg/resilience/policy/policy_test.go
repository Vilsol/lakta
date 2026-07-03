package policy_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/failsafe-go/failsafe-go/timeout"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

const prefix = "modules.resilience.policy.default.policies."

var errFlaky = errors.New("flaky failure")

func setup(t *testing.T, data map[string]any, options ...policy.Option) *policy.Registry {
	t.Helper()
	h := testkit.NewHarness(t)
	m := policy.NewModule(options...)

	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(data, "."), nil))
	testza.AssertNoError(t, m.LoadConfig(k))
	testza.AssertNoError(t, m.Init(h.Ctx()))

	reg, err := do.Invoke[*policy.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	return reg
}

func TestRun_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "flaky.retry.max_attempts": 3,
	})

	var attempts atomic.Int32
	err := reg.Run(t.Context(), "flaky", func(_ context.Context) error {
		if attempts.Add(1) < 3 {
			return errFlaky
		}
		return nil
	})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, int32(3), attempts.Load())
}

func TestRun_RetryExhaustionReturnsError(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "flaky.retry.max_attempts": 2,
	})

	var attempts atomic.Int32
	err := reg.Run(t.Context(), "flaky", func(_ context.Context) error {
		attempts.Add(1)
		return errFlaky
	})

	testza.AssertTrue(t, errors.Is(err, retrypolicy.ErrExceeded) || errors.Is(err, errFlaky))
	testza.AssertEqual(t, int32(2), attempts.Load())
}

func TestRun_TimeoutFires(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "slow.timeout": "50ms",
	})

	err := reg.Run(t.Context(), "slow", func(_ context.Context) error {
		time.Sleep(300 * time.Millisecond)
		return nil
	})

	testza.AssertErrorIs(t, err, timeout.ErrExceeded)
}

func TestRun_TimeoutAppliesPerRetryAttempt(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "combo.retry.max_attempts": 2,
		prefix + "combo.timeout":            "50ms",
	})

	var attempts atomic.Int32
	err := reg.Run(t.Context(), "combo", func(_ context.Context) error {
		if attempts.Add(1) == 1 {
			time.Sleep(300 * time.Millisecond) // first attempt times out
		}
		return nil
	})

	// Timeout is inside retry: attempt 1 times out, attempt 2 succeeds.
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, int32(2), attempts.Load())
}

func TestRun_CircuitBreakerOpensAfterThreshold(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "guarded.circuit_breaker.failure_threshold": 1,
		prefix + "guarded.circuit_breaker.delay":             "1m",
	})

	testza.AssertErrorIs(t, reg.Run(t.Context(), "guarded", func(_ context.Context) error {
		return errFlaky
	}), errFlaky)

	var invoked atomic.Bool
	err := reg.Run(t.Context(), "guarded", func(_ context.Context) error {
		invoked.Store(true)
		return nil
	})

	testza.AssertErrorIs(t, err, circuitbreaker.ErrOpen)
	testza.AssertFalse(t, invoked.Load())
}

func TestRun_RateLimiterRejectsWhenExceeded(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "limited.rate_limit.max":    1,
		prefix + "limited.rate_limit.period": "1m",
		prefix + "limited.rate_limit.bursty": true,
	})

	testza.AssertNoError(t, reg.Run(t.Context(), "limited", func(_ context.Context) error { return nil }))

	err := reg.Run(t.Context(), "limited", func(_ context.Context) error { return nil })
	testza.AssertErrorIs(t, err, ratelimiter.ErrExceeded)
}

func TestGet_ReturnsTypedResult(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "flaky.retry.max_attempts": 2,
	})

	v, err := policy.Get(t.Context(), reg, "flaky", func(_ context.Context) (string, error) {
		return "typed", nil
	})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "typed", v)
}

func TestRun_UnknownPolicyReturnsErrorListingKnown(t *testing.T) {
	t.Parallel()
	reg := setup(t, map[string]any{
		prefix + "flaky.retry.max_attempts": 2,
	})

	err := reg.Run(t.Context(), "missing", func(_ context.Context) error { return nil })

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "flaky")
}

func TestModule_WithPolicyCodeOption(t *testing.T) {
	t.Parallel()
	rp := retrypolicy.NewBuilder[any]().WithMaxAttempts(3).Build()
	reg := setup(t, map[string]any{}, policy.WithPolicy("code_defined", rp))

	var attempts atomic.Int32
	err := reg.Run(t.Context(), "code_defined", func(_ context.Context) error {
		if attempts.Add(1) < 3 {
			return errFlaky
		}
		return nil
	})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, int32(3), attempts.Load())
}

func TestModule_InitFailsOnEmptyPolicy(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := policy.NewModule()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		prefix + "empty.timeout": "0s",
	}, "."), nil))
	testza.AssertNoError(t, m.LoadConfig(k))

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

func TestModule_InitFailsOnInvalidRetry(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := policy.NewModule()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		prefix + "bad.retry.max_attempts": 0,
	}, "."), nil))
	testza.AssertNoError(t, m.LoadConfig(k))

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

func TestModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.resilience.policy.default", policy.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.resilience.policy.custom", policy.NewModule(policy.WithName("custom")).ConfigPath())
}
