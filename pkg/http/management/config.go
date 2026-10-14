package management

import (
	"net/netip"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 8081
)

// Config represents configuration for the management server [Module].
type Config struct {
	// Name is the instance name.
	Name string `koanf:"-"`

	// Host specifies the address for the server to bind to.
	Host string `koanf:"host"`

	// Port represents the port number on which the server listens.
	Port uint16 `koanf:"port"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name: config.DefaultInstanceName,
		Host: defaultHost,
		Port: defaultPort,
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config { return config.Apply(NewDefaultConfig(), options...) }

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// AddrPort returns the parsed address and port for the server.
func (c *Config) AddrPort() (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return netip.AddrPort{}, oops.Wrapf(err, "failed to parse host address")
	}
	return netip.AddrPortFrom(addr, c.Port), nil
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithHost sets the host address.
func WithHost(host string) Option {
	return func(m *Config) { m.Host = host }
}

// WithPort sets the port number.
func WithPort(port uint16) Option {
	return func(m *Config) { m.Port = port }
}
