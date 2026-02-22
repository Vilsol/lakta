package tint

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
)

// Module provides a tint-based slog.Handler via DI.
type Module struct {
	lakta.NamedBase

	config  Config
	handler slog.Handler
}

// NewModule creates a new tint logging module with the given options.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryLogging, "tint", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init loads configuration, creates the tint handler, and registers it in DI.
func (m *Module) Init(ctx context.Context) error {
	m.handler = m.config.NewHandler()

	lakta.ProvideValue[slog.Handler](ctx, m.handler)

	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[slog.Handler](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown is a no-op for this module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}
