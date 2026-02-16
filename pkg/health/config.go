package health

import (
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for health check [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// ComponentName defines the name of the component.
	ComponentName string `koanf:"component_name"`

	// ComponentVersion represents the version of the component.
	ComponentVersion string `koanf:"component_version"`

	// Checks defines a list of health check configurations for the module.
	Checks []health.Config `code_only:"WithCheck" koanf:"-"`
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
	return oops.Wrapf(k.Unmarshal(path, c), "failed to load config from koanf at path %s", path)
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

// WithComponentName sets the component name for health reporting.
func WithComponentName(name string) Option {
	return func(m *Config) { m.ComponentName = name }
}

// WithComponentVersion sets the component version for health reporting.
func WithComponentVersion(version string) Option {
	return func(m *Config) { m.ComponentVersion = version }
}

// WithCheck adds a health check to be registered on initialization (code-only).
func WithCheck(check health.Config) Option {
	return func(m *Config) {
		m.Checks = append(m.Checks, check)
	}
}
