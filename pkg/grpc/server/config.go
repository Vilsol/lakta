package grpcserver

import (
	"net/netip"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 50051
)

// Config represents configuration for GRPC server [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Host specifies the address for the GRPC server to bind to.
	Host string `koanf:"host"`

	// Port represents the port number on which the GRPC server listens.
	Port uint16 `koanf:"port"`

	// HealthCheck determines whether gRPC health checking is enabled or disabled.
	HealthCheck bool `koanf:"health_check"`

	// Services is a map of gRPC service descriptors and their implementations to be registered on the server.
	Services map[*grpc.ServiceDesc]any `code_only:"WithService" koanf:"-"`
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
	return oops.Wrapf(k.Unmarshal(path, c), "failed to load config from koanf at path %s", path)
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

// WithHealthCheck enables or disables the gRPC health check.
func WithHealthCheck(enabled bool) Option {
	return func(m *Config) { m.HealthCheck = enabled }
}

// WithService adds service to the list of services to be registered (code-only).
func WithService(serviceDescriptor *grpc.ServiceDesc, service any) Option {
	return func(m *Config) { m.Services[serviceDescriptor] = service }
}
