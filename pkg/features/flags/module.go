package flags

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Module provides config-driven [Flags] via DI, hot-reloaded on config change.
type Module struct {
	lakta.NamedBase

	config Config
	flags  *Flags
}

// NewModule creates a new feature flags module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryFeatures, "flags", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init parses the flag definitions and provides [Flags] to the injector.
func (m *Module) Init(ctx context.Context) error {
	snap, err := parseSnapshot(m.config.Flags)
	if err != nil {
		return oops.Wrapf(err, "failed to parse feature flags")
	}

	m.flags = newFlags(snap)
	lakta.ProvideValue(ctx, m.flags)

	return nil
}

// OnReload re-parses flag definitions from the reloaded config, keeping the
// previous snapshot if the new one fails to parse.
// The OnReload contract provides no context, so use the default logger.
func (m *Module) OnReload(k *koanf.Koanf) {
	if m.flags == nil {
		return
	}

	// Fresh Config so stale flags from the previous load can't linger.
	cfg := Config{Name: m.config.Name}
	if err := cfg.LoadFromKoanf(k, m.ConfigPath()); err != nil {
		slog.Error("failed to reload feature flags config, keeping previous flags", slog.Any("error", err))
		return
	}
	snap, err := parseSnapshot(cfg.Flags)
	if err != nil {
		slog.Error("failed to parse reloaded feature flags, keeping previous flags", slog.Any("error", err))
		return
	}

	m.config = cfg
	m.flags.swap(snap)
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*Flags](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown is a no-op for the flags module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}
