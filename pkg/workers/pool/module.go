package pool

import (
	"context"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Module provides a [Registry] of named worker pools via DI.
type Module struct {
	lakta.NamedBase

	config   Config
	registry *Registry
}

// NewModule creates a new worker pool module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryWorkers, "pool", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init builds the pools and provides the [Registry] to the injector.
func (m *Module) Init(ctx context.Context) error {
	pools := make(map[string]*Pool)
	for name, cfg := range m.config.MergedPools() {
		pools[name] = New(cfg)
	}

	m.registry = &Registry{pools: pools}
	lakta.ProvideValue(ctx, m.registry)

	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*Registry](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown closes all pools, draining queued tasks.
func (m *Module) Shutdown(ctx context.Context) error {
	return oops.Wrapf(m.registry.close(ctx), "failed to close worker pools")
}
