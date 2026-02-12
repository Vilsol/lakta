package health

import (
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
)

// Config represents configuration for health check [Module]
type Config struct {
	// Instance name (determines config path, cannot come from config file)
	Name string `koanf:"-"`

	// File-configurable fields
	ComponentName    string `koanf:"component_name"`
	ComponentVersion string `koanf:"component_version"`

	// Code-only fields
	Checks []health.Config `koanf:"-"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:             config.DefaultInstanceName,
		ComponentName:    "",
		ComponentVersion: "",
		Checks:           nil,
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
	return k.Unmarshal(path, c)
}

// GetComponent returns the health.Component from config fields.
func (c *Config) GetComponent() health.Component {
	return health.Component{
		Name:    c.ComponentName,
		Version: c.ComponentVersion,
	}
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithCheck adds a health check to be registered on initialization (code-only).
func WithCheck(check health.Config) Option {
	return func(m *Config) {
		m.Checks = append(m.Checks, check)
	}
}
