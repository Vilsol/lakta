package grpcclient

import (
	"context"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultTarget = "localhost:50051"
)

// ClientRegistrar registers typed gRPC clients against a connection.
type ClientRegistrar func(ctx context.Context, conn *grpc.ClientConn)

// Config holds gRPC client connection settings.
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Target specifies the target address for the gRPC client connection.
	Target string `koanf:"target"`

	// Insecure determines whether transport credentials should use an insecure configuration.
	Insecure bool `koanf:"insecure"`

	// Credentials specifies the transport credentials for the gRPC connection; ignored if Insecure is true.
	Credentials credentials.TransportCredentials `code_only:"true" koanf:"-"`

	// ClientRegistrars contains a list of functions to register typed gRPC clients with a client connection during setup.
	ClientRegistrars []ClientRegistrar `code_only:"WithClient" koanf:"-"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:             config.DefaultInstanceName,
		Target:           defaultTarget,
		ClientRegistrars: make([]ClientRegistrar, 0),
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

// GetCredentials returns the transport credentials, applying Insecure if set.
func (c *Config) GetCredentials() credentials.TransportCredentials { //nolint:ireturn
	if c.Credentials != nil {
		return c.Credentials
	}
	if c.Insecure {
		return insecure.NewCredentials()
	}
	return nil
}

// DialOptions returns grpc.DialOption slice for creating a client connection.
func (c *Config) DialOptions() []grpc.DialOption {
	opts := []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
	if creds := c.GetCredentials(); creds != nil {
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}
	return opts
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithTarget sets the target address for the gRPC client.
func WithTarget(target string) Option {
	return func(m *Config) { m.Target = target }
}

// WithInsecure enables or disables insecure transport credentials.
func WithInsecure(insecure bool) Option {
	return func(m *Config) { m.Insecure = insecure }
}

// WithClient registers a typed client constructor (code-only).
func WithClient[T any](constructor func(grpc.ClientConnInterface) T) Option {
	return func(m *Config) {
		m.ClientRegistrars = append(m.ClientRegistrars, func(ctx context.Context, conn *grpc.ClientConn) {
			lakta.Provide(ctx, func(_ do.Injector) (T, error) {
				return constructor(conn), nil
			})
		})
	}
}
