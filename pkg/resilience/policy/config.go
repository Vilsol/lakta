package policy

import (
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/hedgepolicy"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/failsafe-go/failsafe-go/timeout"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for the resilience policy [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Policies defines the named policies this module manages. Prefer
	// snake_case names; hyphens cannot be overridden via environment
	// variables.
	Policies map[string]PolicyConfig `koanf:"policies"`

	// CodePolicies holds policies registered via WithPolicy (code-only). A
	// config entry with the same name replaces it wholesale.
	CodePolicies map[string][]failsafe.Policy[any] `code_only:"WithPolicy" koanf:"-"`
}

// PolicyConfig defines one named policy chain. Primitives compose in a fixed
// order, outermost to innermost: hedge, retry, circuit breaker, rate limit,
// adaptive limiter, bulkhead, timeout.
type PolicyConfig struct {
	// Timeout bounds each execution attempt. Zero disables it.
	Timeout time.Duration `koanf:"timeout"`

	// Retry retries failed executions.
	Retry *RetryConfig `koanf:"retry"`

	// CircuitBreaker rejects executions while failures exceed a threshold.
	CircuitBreaker *BreakerConfig `koanf:"circuit_breaker"`

	// RateLimit bounds the execution rate.
	RateLimit *RateLimitConfig `koanf:"rate_limit"`

	// Hedge starts redundant attempts after a delay to trim tail latency.
	Hedge *HedgeConfig `koanf:"hedge"`

	// AdaptiveLimiter sheds load by self-tuning a concurrency limit.
	AdaptiveLimiter *AdaptiveLimiterConfig `koanf:"adaptive_limiter"`

	// Bulkhead caps concurrency with a hard ceiling nearest the call.
	Bulkhead *BulkheadConfig `koanf:"bulkhead"`
}

// RetryConfig configures the retry primitive.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts, including the first.
	MaxAttempts int `koanf:"max_attempts"`

	// Delay is the base delay between attempts. Zero means immediate retry.
	Delay time.Duration `koanf:"delay"`

	// MaxDelay caps exponential backoff; requires Delay. Zero keeps the
	// delay fixed.
	MaxDelay time.Duration `koanf:"max_delay"`

	// Jitter randomizes each delay by up to this duration.
	Jitter time.Duration `koanf:"jitter"`
}

// BreakerConfig configures the circuit breaker primitive.
type BreakerConfig struct {
	// FailureThreshold is the number of failures that opens the breaker.
	FailureThreshold int `koanf:"failure_threshold"`

	// SuccessThreshold is the number of half-open successes that close it.
	SuccessThreshold int `koanf:"success_threshold"`

	// Delay is how long the breaker stays open before half-opening.
	Delay time.Duration `koanf:"delay"`
}

// RateLimitConfig configures the rate limiter primitive.
type RateLimitConfig struct {
	// Max is the number of executions allowed per period.
	Max int `koanf:"max"`

	// Period is the window Max applies to. Defaults to one second.
	Period time.Duration `koanf:"period"`

	// Bursty allows Max executions at once instead of smoothing them
	// evenly across the period.
	Bursty bool `koanf:"bursty"`

	// MaxWait is how long an execution may wait for a permit before being
	// rejected. Zero rejects immediately.
	MaxWait time.Duration `koanf:"max_wait"`
}

// HedgeConfig configures the hedge primitive.
type HedgeConfig struct {
	// Delay before starting a hedged attempt. Required (> 0).
	Delay time.Duration `koanf:"delay"`

	// MaxHedges is the max number of hedged attempts. 0 = library default (1).
	MaxHedges int `koanf:"max_hedges"`
}

// AdaptiveLimiterConfig configures the adaptive concurrency limiter (soft shed point).
type AdaptiveLimiterConfig struct {
	// Min is the minimum concurrency limit.
	Min uint `koanf:"min"`

	// Max is the maximum concurrency limit; must be >= 1.
	Max uint `koanf:"max"`

	// Initial is the starting limit; Min <= Initial <= Max.
	Initial uint `koanf:"initial"`

	// MaxWait is how long to wait for a permit before rejecting. Zero rejects
	// immediately.
	MaxWait time.Duration `koanf:"max_wait"`

	// Queueing enables absorbing short spikes before rejecting. Nil disables it.
	Queueing *QueueingConfig `koanf:"queueing"`
}

// QueueingConfig maps to adaptivelimiter Builder.WithQueueing(initial, max) rejection factors.
type QueueingConfig struct {
	// InitialFactor is the queue depth (times the limit) before rejections
	// begin. Must be >= 1; the library panics below 1.
	InitialFactor float64 `koanf:"initial_factor"`

	// MaxFactor is the queue depth (times the limit) at which all excess is
	// rejected. Must be >= InitialFactor.
	MaxFactor float64 `koanf:"max_factor"`
}

// BulkheadConfig configures the bulkhead (hard concurrency ceiling nearest the call).
type BulkheadConfig struct {
	// MaxConcurrent is the hard concurrency ceiling; must be >= 1.
	MaxConcurrent uint `koanf:"max_concurrent"`

	// MaxWait is how long to wait for a slot before rejecting. Zero rejects
	// immediately.
	MaxWait time.Duration `koanf:"max_wait"`
}

// Build produces the failsafe policy chain, ordered outermost to innermost:
// hedge, retry, circuit breaker, rate limit, adaptive limiter, bulkhead, timeout.
func (pc *PolicyConfig) Build() ([]failsafe.Policy[any], error) {
	policies, _, err := pc.buildWithMetrics("", nil)
	return policies, err
}

// buildWithMetrics builds the chain and, when pm != nil, wires shed/hedge
// counter listeners for the named policy. It returns the adaptive limiter's
// Metrics (nil when no adaptive_limiter block) so the module can register the
// limit/inflight observable gauges. The append order encodes the composition
// order: hedge, retry, circuit breaker, rate limit, adaptive limiter, bulkhead,
// timeout (first appended = outermost).
//
//nolint:ireturn // returns the adaptivelimiter.Metrics interface so the module can read limit/inflight for gauges
func (pc *PolicyConfig) buildWithMetrics(name string, pm *policyMetrics) ([]failsafe.Policy[any], adaptivelimiter.Metrics, error) {
	var policies []failsafe.Policy[any]

	if pc.Hedge != nil {
		hedge, err := pc.Hedge.build(pm.onHedge(name))
		if err != nil {
			return nil, nil, err
		}
		policies = append(policies, hedge)
	}

	if pc.Retry != nil {
		if pc.Retry.MaxAttempts < 1 {
			return nil, nil, oops.Errorf("retry: max_attempts must be at least 1")
		}
		b := retrypolicy.NewBuilder[any]().WithMaxAttempts(pc.Retry.MaxAttempts)
		switch {
		case pc.Retry.Delay > 0 && pc.Retry.MaxDelay > 0:
			b = b.WithBackoff(pc.Retry.Delay, pc.Retry.MaxDelay)
		case pc.Retry.Delay > 0:
			b = b.WithDelay(pc.Retry.Delay)
		}
		if pc.Retry.Jitter > 0 {
			b = b.WithJitter(pc.Retry.Jitter)
		}
		policies = append(policies, b.Build())
	}

	if pc.CircuitBreaker != nil {
		if pc.CircuitBreaker.FailureThreshold < 1 {
			return nil, nil, oops.Errorf("circuit_breaker: failure_threshold must be at least 1")
		}
		b := circuitbreaker.NewBuilder[any]().WithFailureThreshold(uint(pc.CircuitBreaker.FailureThreshold))
		if pc.CircuitBreaker.SuccessThreshold > 0 {
			b = b.WithSuccessThreshold(uint(pc.CircuitBreaker.SuccessThreshold))
		}
		if pc.CircuitBreaker.Delay > 0 {
			b = b.WithDelay(pc.CircuitBreaker.Delay)
		}
		policies = append(policies, b.Build())
	}

	if pc.RateLimit != nil {
		limiter, err := pc.RateLimit.build()
		if err != nil {
			return nil, nil, err
		}
		policies = append(policies, limiter)
	}

	var limiterMetrics adaptivelimiter.Metrics
	if pc.AdaptiveLimiter != nil {
		limiter, err := pc.AdaptiveLimiter.build(pm.onShed(name, "adaptive_limiter"))
		if err != nil {
			return nil, nil, err
		}
		limiterMetrics = limiter
		policies = append(policies, limiter)
	}

	if pc.Bulkhead != nil {
		bh, err := pc.Bulkhead.build(pm.onShed(name, "bulkhead"))
		if err != nil {
			return nil, nil, err
		}
		policies = append(policies, bh)
	}

	if pc.Timeout > 0 {
		policies = append(policies, timeout.New[any](pc.Timeout))
	}

	if len(policies) == 0 {
		return nil, nil, oops.Errorf("policy defines no primitives")
	}
	return policies, limiterMetrics, nil
}

func (rl *RateLimitConfig) build() (failsafe.Policy[any], error) {
	if rl.Max < 1 {
		return nil, oops.Errorf("rate_limit: max must be at least 1")
	}
	period := rl.Period
	if period <= 0 {
		period = time.Second
	}

	var b ratelimiter.Builder[any]
	if rl.Bursty {
		b = ratelimiter.NewBurstyBuilder[any](uint(rl.Max), period)
	} else {
		b = ratelimiter.NewSmoothBuilder[any](uint(rl.Max), period)
	}
	if rl.MaxWait > 0 {
		b = b.WithMaxWaitTime(rl.MaxWait)
	}
	return b.Build(), nil
}

// build validates and constructs the hedge policy. onHedge is nil when otel is absent.
func (hc *HedgeConfig) build(onHedge func(failsafe.ExecutionEvent[any])) (failsafe.Policy[any], error) {
	if hc.Delay <= 0 {
		return nil, oops.Errorf("hedge: delay must be greater than zero")
	}
	if hc.MaxHedges < 0 {
		return nil, oops.Errorf("hedge: max_hedges must not be negative")
	}
	b := hedgepolicy.NewBuilderWithDelay[any](hc.Delay)
	if onHedge != nil {
		b = b.OnHedge(onHedge)
	}
	if hc.MaxHedges > 0 {
		b = b.WithMaxHedges(hc.MaxHedges)
	}
	return b.Build(), nil
}

// build validates and constructs the adaptive limiter. onExceeded is nil when
// otel is absent. It returns the concrete limiter so the module can read its
// Metrics for gauges.
func (ac *AdaptiveLimiterConfig) build(onExceeded func(failsafe.ExecutionEvent[any])) (adaptivelimiter.AdaptiveLimiter[any], error) {
	if ac.Max < 1 {
		return nil, oops.Errorf("adaptive_limiter: max must be at least 1")
	}
	if ac.Min > ac.Initial || ac.Initial > ac.Max {
		return nil, oops.Errorf("adaptive_limiter: require min <= initial <= max (min=%d initial=%d max=%d)", ac.Min, ac.Initial, ac.Max)
	}
	if ac.Queueing != nil {
		if ac.Queueing.InitialFactor < 1 {
			return nil, oops.Errorf("adaptive_limiter: queueing.initial_factor must be at least 1")
		}
		if ac.Queueing.MaxFactor < ac.Queueing.InitialFactor {
			return nil, oops.Errorf("adaptive_limiter: queueing.max_factor must be at least initial_factor")
		}
	}

	b := adaptivelimiter.NewBuilder[any]().WithLimits(ac.Min, ac.Max, ac.Initial)
	if onExceeded != nil {
		b = b.OnLimitExceeded(onExceeded)
	}
	if ac.Queueing != nil {
		b = b.WithQueueing(ac.Queueing.InitialFactor, ac.Queueing.MaxFactor)
	}
	if ac.MaxWait > 0 {
		b = b.WithMaxWaitTime(ac.MaxWait)
	}
	return b.Build(), nil
}

// build validates and constructs the bulkhead. onFull is nil when otel is absent.
func (bc *BulkheadConfig) build(onFull func(failsafe.ExecutionEvent[any])) (bulkhead.Bulkhead[any], error) {
	if bc.MaxConcurrent < 1 {
		return nil, oops.Errorf("bulkhead: max_concurrent must be at least 1")
	}
	b := bulkhead.NewBuilder[any](bc.MaxConcurrent)
	if onFull != nil {
		b = b.OnFull(onFull)
	}
	if bc.MaxWait > 0 {
		b = b.WithMaxWaitTime(bc.MaxWait)
	}
	return b.Build(), nil
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name: config.DefaultInstanceName,
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return config.Apply(NewDefaultConfig(), options...)
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithPolicy registers a policy chain in code (code-only), ordered outermost
// first; config with the same name takes precedence. Use for primitives not
// expressible in config (fallback), or hand-built prioritized limiters.
func WithPolicy(name string, policies ...failsafe.Policy[any]) Option {
	return func(m *Config) {
		if m.CodePolicies == nil {
			m.CodePolicies = make(map[string][]failsafe.Policy[any])
		}
		m.CodePolicies[name] = policies
	}
}
