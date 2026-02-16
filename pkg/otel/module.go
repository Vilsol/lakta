package otel

import (
	"context"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

// Module manages OpenTelemetry SDK lifecycle.
type Module struct {
	config     Config
	onShutdown func(context.Context) error
}

// NewModule creates a new OTEL module
func NewModule(options ...Option) *Module {
	return &Module{config: NewConfig(options...)}
}

// Name returns the instance name.
func (m *Module) Name() string {
	return m.config.Name
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryOTel, "otel", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	path := m.ConfigPath()
	if k.Exists(path) {
		return m.config.LoadFromKoanf(k, path)
	}
	return nil
}

// Init sets up the entire OTEL provider and exporter stack
func (m *Module) Init(ctx context.Context) error {
	// Load config from koanf if available
	if k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx)); err == nil {
		if err := m.LoadConfig(k); err != nil {
			return oops.Wrapf(err, "failed to load config")
		}
	}

	var err error
	m.onShutdown, err = setupOTelSDK(ctx, m.config.ServiceName)
	if err != nil {
		return oops.Wrapf(err, "failed to setup OpenTelemetry SDK")
	}

	return nil
}

// Shutdown gracefully stops the OTEL exporters
func (m *Module) Shutdown(ctx context.Context) error {
	return m.onShutdown(ctx)
}
