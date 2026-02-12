package temporal

import (
	"context"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultTarget = "localhost:7233"
)

type Registrar func(ctx context.Context, worker worker.Worker) error

type Config struct {
	// Instance name (determines config path, cannot come from config file)
	Name string `koanf:"-"`

	// File-configurable fields
	Target    string `koanf:"target"`
	TaskQueue string `koanf:"task_queue"`
	Namespace string `koanf:"namespace"`
	Insecure  bool   `koanf:"insecure"`

	// Code-only fields
	Credentials credentials.TransportCredentials `koanf:"-"`
	Registrars  []Registrar                      `koanf:"-"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:       config.DefaultInstanceName,
		Target:     defaultTarget,
		Registrars: make([]Registrar, 0),
		Namespace:  "default",
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

// GetCredentials returns the transport credentials, applying Insecure if set.
func (c *Config) GetCredentials() credentials.TransportCredentials {
	if c.Credentials != nil {
		return c.Credentials
	}
	if c.Insecure {
		return insecure.NewCredentials()
	}
	return nil
}

// DialOptions returns grpc.DialOption slice for the temporal client connection.
func (c *Config) DialOptions() []grpc.DialOption {
	opts := []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
	if creds := c.GetCredentials(); creds != nil {
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}
	return opts
}

// ClientOptions returns temporal client.Options with config values applied.
func (c *Config) ClientOptions(logger log.Logger, interceptors []interceptor.ClientInterceptor) client.Options {
	return client.Options{
		HostPort:     c.Target,
		Namespace:    c.Namespace,
		Logger:       logger,
		Interceptors: interceptors,
		ConnectionOptions: client.ConnectionOptions{
			DialOptions: c.DialOptions(),
		},
	}
}

// WorkerOptions returns temporal worker.Options with config values applied.
func (c *Config) WorkerOptions(ctx context.Context, interceptors []interceptor.WorkerInterceptor) worker.Options {
	return worker.Options{
		BackgroundActivityContext: ctx,
		Interceptors:              interceptors,
	}
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithRegistrar adds a workflow/activity registrar (code-only).
func WithRegistrar(registrar Registrar) Option {
	return func(m *Config) { m.Registrars = append(m.Registrars, registrar) }
}
