package grpcclient

import (
	"context"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const (
	defaultTarget           = "localhost:50051"
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 20 * time.Second
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

	// TLS configures file-path based transport security (client cert for mutual
	// TLS, CA bundle to verify the server). Ignored if Insecure or Credentials is set.
	TLS config.TLS `koanf:"tls"`

	// Credentials specifies the transport credentials for the gRPC connection,
	// e.g. a SPIFFE/SPIRE source; takes precedence over Insecure and TLS.
	Credentials credentials.TransportCredentials `code_only:"WithCredentials" koanf:"-"`

	// ClientRegistrars contains a list of functions to register typed gRPC clients with a client connection during setup.
	ClientRegistrars []ClientRegistrar `code_only:"WithClient" koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name:             config.DefaultInstanceName,
		Target:           defaultTarget,
		ClientRegistrars: make([]ClientRegistrar, 0),
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

// GetCredentials resolves the transport credentials: explicit Credentials win,
// then Insecure, then TLS file paths, otherwise nil (gRPC then rejects the dial
// unless the caller supplies credentials, i.e. fail-closed).
func (c *Config) GetCredentials() (credentials.TransportCredentials, error) { //nolint:ireturn
	if c.Credentials != nil {
		return c.Credentials, nil
	}
	if c.Insecure {
		return insecure.NewCredentials(), nil
	}

	tlsCfg, err := c.TLS.ClientConfig()
	if err != nil {
		return nil, oops.Wrapf(err, "failed to build client TLS config")
	}
	if tlsCfg != nil {
		return credentials.NewTLS(tlsCfg), nil
	}

	return nil, nil
}

// KeepaliveParams returns generous client keepalive parameters.
func (c *Config) KeepaliveParams() keepalive.ClientParameters {
	return keepalive.ClientParameters{
		Time:                defaultKeepaliveTime,
		Timeout:             defaultKeepaliveTimeout,
		PermitWithoutStream: false,
	}
}

// DialOptions returns grpc.DialOption slice for creating a client connection.
func (c *Config) DialOptions() ([]grpc.DialOption, error) {
	opts := []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithKeepaliveParams(c.KeepaliveParams()),
	}

	creds, err := c.GetCredentials()
	if err != nil {
		return nil, err
	}
	if creds != nil {
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}

	return opts, nil
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

// WithCredentials sets explicit transport credentials, overriding Insecure and
// TLS file config. Use for in-process sources such as SPIFFE/SPIRE (code-only).
func WithCredentials(creds credentials.TransportCredentials) Option {
	return func(m *Config) { m.Credentials = creds }
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
