package policy

import (
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
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
// order, outermost to innermost: retry, circuit breaker, rate limit, timeout.
type PolicyConfig struct {
	// Timeout bounds each execution attempt. Zero disables it.
	Timeout time.Duration `koanf:"timeout"`

	// Retry retries failed executions.
	Retry *RetryConfig `koanf:"retry"`

	// CircuitBreaker rejects executions while failures exceed a threshold.
	CircuitBreaker *BreakerConfig `koanf:"circuit_breaker"`

	// RateLimit bounds the execution rate.
	RateLimit *RateLimitConfig `koanf:"rate_limit"`
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

// Build produces the failsafe policy chain, ordered outermost to innermost:
// retry, circuit breaker, rate limit, timeout.
func (pc *PolicyConfig) Build() ([]failsafe.Policy[any], error) {
	var policies []failsafe.Policy[any]

	if pc.Retry != nil {
		if pc.Retry.MaxAttempts < 1 {
			return nil, oops.Errorf("retry: max_attempts must be at least 1")
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
			return nil, oops.Errorf("circuit_breaker: failure_threshold must be at least 1")
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
			return nil, err
		}
		policies = append(policies, limiter)
	}

	if pc.Timeout > 0 {
		policies = append(policies, timeout.New[any](pc.Timeout))
	}

	if len(policies) == 0 {
		return nil, oops.Errorf("policy defines no primitives")
	}
	return policies, nil
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
// expressible in config (bulkhead, hedge, fallback).
func WithPolicy(name string, policies ...failsafe.Policy[any]) Option {
	return func(m *Config) {
		if m.CodePolicies == nil {
			m.CodePolicies = make(map[string][]failsafe.Policy[any])
		}
		m.CodePolicies[name] = policies
	}
}
