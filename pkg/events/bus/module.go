package bus

import (
	"context"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
)

// Module provides an in-process typed event [Bus] via DI.
type Module struct {
	lakta.NamedBase

	config Config
	bus    *Bus
}

// NewModule creates a new event bus module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryEvents, "bus", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init creates the bus and provides it to the injector.
func (m *Module) Init(ctx context.Context) error {
	m.bus = NewBus(m.config.BufferSize)
	lakta.ProvideValue(ctx, m.bus)
	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*Bus](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown closes the bus, draining queued async events.
func (m *Module) Shutdown(ctx context.Context) error {
	return m.bus.Close(ctx)
}
