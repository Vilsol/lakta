package grpcclient

import (
	"context"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

// Module manages a gRPC client connection lifecycle.
type Module struct {
	config Config
	conn   *grpc.ClientConn
}

// NewModule creates a new gRPC client module with the given options.
func NewModule(options ...Option) *Module {
	return &Module{config: NewConfig(options...)}
}

// Name returns the instance name.
func (m *Module) Name() string {
	return m.config.Name
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryGRPC, "client", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	path := m.ConfigPath()
	if k.Exists(path) {
		return m.config.LoadFromKoanf(k, path)
	}
	return nil
}

// Init loads configuration, creates the gRPC connection, and registers typed clients.
func (m *Module) Init(ctx context.Context) error {
	// Load config from koanf if available
	if k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx)); err == nil {
		if err := m.LoadConfig(k); err != nil {
			return oops.Wrapf(err, "failed to load config")
		}
	}

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

// Shutdown closes the gRPC client connection.
func (m *Module) Shutdown(_ context.Context) error {
	return oops.Wrapf(m.conn.Close(), "failed to close gRPC client connection")
}
