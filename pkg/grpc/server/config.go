package grpcserver

import (
	"net/netip"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 50051

	defaultMaxConnectionIdle  = 5 * time.Minute
	defaultKeepaliveTime      = 2 * time.Hour
	defaultKeepaliveTimeout   = 20 * time.Second
	defaultEnforcementMinTime = 30 * time.Second
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

	// TLS configures file-path based transport security. When unset the server
	// listens in plaintext.
	TLS config.TLS `koanf:"tls"`

	// Services is a map of gRPC service descriptors and their implementations to be registered on the server.
	Services map[*grpc.ServiceDesc]any `code_only:"WithService" koanf:"-"`

	// Credentials overrides TLS with explicit transport credentials, e.g. a
	// SPIFFE/SPIRE source via credentials.NewTLS(tlsconfig.MTLSServerConfig(...)).
	// Takes precedence over TLS when set.
	Credentials credentials.TransportCredentials `code_only:"WithCredentials" koanf:"-"`
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
	return config.Apply(NewDefaultConfig(), options...)
}

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

// WithHealthCheck enables or disables the gRPC health check.
func WithHealthCheck(enabled bool) Option {
	return func(m *Config) { m.HealthCheck = enabled }
}

// WithService adds service to the list of services to be registered (code-only).
func WithService(serviceDescriptor *grpc.ServiceDesc, service any) Option {
	return func(m *Config) { m.Services[serviceDescriptor] = service }
}

// WithCredentials sets explicit transport credentials, overriding TLS file
// config. Use for in-process sources such as SPIFFE/SPIRE (code-only).
func WithCredentials(creds credentials.TransportCredentials) Option {
	return func(m *Config) { m.Credentials = creds }
}

// ServerCredentials resolves the transport credentials for the server:
// explicit Credentials win, otherwise TLS file paths are loaded, otherwise nil
// (plaintext).
func (c *Config) ServerCredentials() (credentials.TransportCredentials, error) { //nolint:ireturn
	if c.Credentials != nil {
		return c.Credentials, nil
	}

	tlsCfg, err := c.TLS.ServerConfig()
	if err != nil {
		return nil, oops.Wrapf(err, "failed to build server TLS config")
	}
	if tlsCfg == nil {
		return nil, nil
	}

	return credentials.NewTLS(tlsCfg), nil
}

// KeepaliveServerParameters returns generous keepalive parameters for the server.
func (c *Config) KeepaliveServerParameters() keepalive.ServerParameters {
	return keepalive.ServerParameters{
		MaxConnectionIdle: defaultMaxConnectionIdle,
		Time:              defaultKeepaliveTime,
		Timeout:           defaultKeepaliveTimeout,
	}
}

// KeepaliveEnforcementPolicy returns the server's keepalive enforcement policy.
func (c *Config) KeepaliveEnforcementPolicy() keepalive.EnforcementPolicy {
	return keepalive.EnforcementPolicy{
		MinTime:             defaultEnforcementMinTime,
		PermitWithoutStream: true,
	}
}
