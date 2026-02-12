package health

import (
	"context"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

// Module provides health check functionality using hellofresh/health-go
type Module struct {
	config Config
	health *health.Health
}

// NewModule creates a new health check module
func NewModule(options ...Option) *Module {
	return &Module{config: NewConfig(options...)}
}

// Name returns the instance name.
func (m *Module) Name() string {
	return m.config.Name
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHealth, "health", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	path := m.ConfigPath()
	if k.Exists(path) {
		return m.config.LoadFromKoanf(k, path)
	}
	return nil
}

// Init creates the health instance and provides it to the injector
func (m *Module) Init(ctx context.Context) error {
	// Load config from koanf if available
	if k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx)); err == nil {
		if err := m.LoadConfig(k); err != nil {
			return oops.Wrapf(err, "failed to load config")
		}
	}

	opts := []health.Option{
		health.WithComponent(m.config.GetComponent()),
	}

	for _, check := range m.config.Checks {
		opts = append(opts, health.WithChecks(check))
	}

	h, err := health.New(opts...)
	if err != nil {
		return oops.Wrapf(err, "failed to create health instance")
	}

	m.health = h

	lakta.Provide(ctx, m.getHealth)

	return nil
}

// Shutdown is a no-op for the health module
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

func (m *Module) getHealth(_ do.Injector) (*health.Health, error) {
	return m.health, nil
}
