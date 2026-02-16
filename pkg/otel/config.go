package otel

import (
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for OTEL [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// ServiceName specifies the OpenTelemetry service name.
	ServiceName string `koanf:"service_name"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:        config.DefaultInstanceName,
		ServiceName: "lakta",
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	cfg := NewDefaultConfig()
	for _, option := range options {
		option(&cfg)
	}
	return cfg
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(k.Unmarshal(path, c), "failed to load config from koanf at path %s", path)
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithServiceName sets the OpenTelemetry service name.
func WithServiceName(name string) Option {
	return func(m *Config) { m.ServiceName = name }
}
