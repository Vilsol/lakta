package grpcclient

import (
	"context"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

// Module manages a gRPC client connection lifecycle.
type Module struct {
	lakta.NamedBase

	config Config
	conn   *grpc.ClientConn
}

// NewModule creates a new gRPC client module with the given options.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryGRPC, "client", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init loads configuration, creates the gRPC connection, and registers typed clients.
func (m *Module) Init(ctx context.Context) error {
	// Create connection using config-provided options
	conn, err := grpc.NewClient(m.config.Target, m.config.DialOptions()...)
	if err != nil {
		return oops.Wrap(err)
	}
	m.conn = conn

	// Register typed clients from config
	for _, register := range m.config.ClientRegistrars {
		register(ctx, conn)
	}

	return nil
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown closes the gRPC client connection.
func (m *Module) Shutdown(_ context.Context) error {
	return oops.Wrapf(m.conn.Close(), "failed to close gRPC client connection")
}
