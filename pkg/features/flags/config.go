package flags

import (
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for the feature flags [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Flags holds the raw flag definitions: scalars for plain values, or
	// {value, rollout} objects for percentage rollouts. Prefer snake_case
	// flag names; hyphens cannot be overridden via environment variables.
	Flags map[string]any `koanf:"flags"`
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
