package spiffe

import (
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Config represents configuration for the SPIFFE [Module].
type Config struct {
	// Name is the instance name.
	Name string `koanf:"-"`

	// SocketPath is the Workload API address, e.g. unix:///spiffe-workload-api/spire-agent.sock.
	SocketPath string `koanf:"socket_path"`

	// TrustDomain is the trust domain peers must belong to.
	TrustDomain string `koanf:"trust_domain"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{Name: config.DefaultInstanceName}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config { return config.Apply(NewDefaultConfig(), options...) }

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// Option configures the Module.
type Option func(*Config)

// WithName sets the instance name for this module.
func WithName(name string) Option { return func(c *Config) { c.Name = name } }

// WithSocketPath sets the Workload API address.
func WithSocketPath(path string) Option { return func(c *Config) { c.SocketPath = path } }

// WithTrustDomain sets the trust domain.
func WithTrustDomain(td string) Option { return func(c *Config) { c.TrustDomain = td } }
