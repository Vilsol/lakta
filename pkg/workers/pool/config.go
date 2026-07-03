package pool

import (
	"maps"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for the worker pool [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Pools defines the named pools this module manages. Prefer snake_case
	// pool names; hyphens cannot be overridden via environment variables.
	Pools map[string]PoolConfig `koanf:"pools"`

	// CodePools holds pools registered via WithPool (code-only). A config
	// entry with the same name replaces it wholesale.
	CodePools map[string]PoolConfig `code_only:"WithPool" koanf:"-"`
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

// MergedPools combines code-registered and config-defined pools; config wins
// per name.
func (c *Config) MergedPools() map[string]PoolConfig {
	merged := make(map[string]PoolConfig, len(c.CodePools)+len(c.Pools))
	maps.Copy(merged, c.CodePools)
	maps.Copy(merged, c.Pools)
	return merged
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithPool registers a pool in code (code-only); config with the same name
// takes precedence.
func WithPool(name string, cfg PoolConfig) Option {
	return func(m *Config) {
		if m.CodePools == nil {
			m.CodePools = make(map[string]PoolConfig)
		}
		m.CodePools[name] = cfg
	}
}
