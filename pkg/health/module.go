package health

import (
	"context"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Module provides health check functionality using hellofresh/health-go
type Module struct {
	lakta.NamedBase

	config Config
	health *health.Health
}

// NewModule creates a new health check module
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHealth, "health", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init creates the health instance and provides it to the injector
func (m *Module) Init(ctx context.Context) error {
	opts := make([]health.Option, 0, 1+len(m.config.Checks))
	opts = append(opts, health.WithComponent(m.config.GetComponent()))

	for _, check := range m.config.Checks {
		opts = append(opts, health.WithChecks(check))
	}

	h, err := health.New(opts...)
	if err != nil {
		return oops.Wrapf(err, "failed to create health instance")
	}

	m.health = h

	lakta.ProvideValue(ctx, m.health)

	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*health.Health](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown is a no-op for the health module
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}
