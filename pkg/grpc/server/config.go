package grpcserver

import (
	"net/netip"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"google.golang.org/grpc"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 50051
)

// Config represents configuration for GRPC server [Module]
type Config struct {
	// Instance name (determines config path, cannot come from config file)
	Name string `koanf:"-"`

	// File-configurable fields
	Host        string `koanf:"host"`
	Port        uint16 `koanf:"port"`
	HealthCheck bool   `koanf:"health_check"`

	// Code-only fields
	Services map[*grpc.ServiceDesc]any `koanf:"-"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:     config.DefaultInstanceName,
		Host:     defaultHost,
		Port:     defaultPort,
		Services: make(map[*grpc.ServiceDesc]any),
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

// AddrPort returns the parsed address and port for the server.
func (c *Config) AddrPort() (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(addr, c.Port), nil
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithService adds service to the list of services to be registered (code-only).
func WithService(serviceDescriptor *grpc.ServiceDesc, service any) Option {
	return func(m *Config) { m.Services[serviceDescriptor] = service }
}
