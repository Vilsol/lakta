package bus

import (
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for the event bus [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// BufferSize is the queue capacity for each async subscription.
	BufferSize int `koanf:"buffer_size"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:       config.DefaultInstanceName,
		BufferSize: defaultBufferSize,
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

// WithBufferSize sets the queue capacity for async subscriptions.
func WithBufferSize(size int) Option {
	return func(m *Config) { m.BufferSize = size }
}
