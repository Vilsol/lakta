package config

import (
	"context"
	"strings"
	"sync"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"github.com/spf13/pflag"
)

var _ lakta.Module = (*Module)(nil)

// IsConfigModule is a marker method to identify this module as the config module.
func (m *Module) IsConfigModule() {}

// Module is the configuration module that loads and provides configuration.
type Module struct {
	config      Config
	koanf       *koanf.Koanf
	mu          sync.RWMutex
	configFiles []configFile
	flagSet     *pflag.FlagSet
	onReload    []func()
}

// NewModule creates a new config module.
func NewModule(options ...Option) *Module {
	return &Module{
		config: NewConfig(options...),
		koanf:  koanf.New("."),
	}
}

// Init initializes the config module, loading configuration from files, env vars, and CLI flags.
func (m *Module) Init(ctx context.Context) error {
	if err := m.loadConfigFiles(m.koanf); err != nil {
		return oops.Wrapf(err, "failed to load config files")
	}

	if err := m.loadEnvVars(); err != nil {
		return oops.Wrapf(err, "failed to load environment variables")
	}

	if err := m.loadCLIFlags(); err != nil {
		return oops.Wrapf(err, "failed to load CLI flags")
	}

	m.startWatcher(ctx)

	lakta.Provide(ctx, m.provideKoanf)
	lakta.Provide(ctx, m.provideReloadNotifier)

	return nil
}

func (m *Module) loadEnvVars() error {
	prefix := m.config.EnvPrefix
	err := m.koanf.Load(env.Provider(prefix, ".", func(s string) string {
		// LAKTA_MODULES_GRPC_SERVER_DEFAULT_PORT -> modules.grpc.server.default.port
		return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, prefix), "_", "."))
	}), nil)
	if err != nil {
		return oops.Wrapf(err, "failed to load env vars")
	}
	return nil
}

func (m *Module) loadCLIFlags() error {
	if m.config.Args == nil {
		return nil
	}

	m.flagSet = pflag.NewFlagSet("config", pflag.ContinueOnError)

	// Pre-populate flags from existing koanf keys so posflag can override them
	for _, key := range m.koanf.Keys() {
		val := m.koanf.Get(key)
		switch v := val.(type) {
		case string:
			m.flagSet.String(key, v, "")
		case int:
			m.flagSet.Int(key, v, "")
		case int64:
			m.flagSet.Int64(key, v, "")
		case float64:
			m.flagSet.Float64(key, v, "")
		case bool:
			m.flagSet.Bool(key, v, "")
		default:
			m.flagSet.String(key, "", "")
		}
	}

	if err := m.flagSet.Parse(m.config.Args); err != nil {
		return oops.Wrapf(err, "failed to parse CLI flags")
	}

	if err := m.koanf.Load(posflag.Provider(m.flagSet, ".", m.koanf), nil); err != nil {
		return oops.Wrapf(err, "failed to load CLI flags into koanf")
	}
	return nil
}

// Shutdown gracefully shuts down the config module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

func (m *Module) provideKoanf(_ do.Injector) (*koanf.Koanf, error) {
	return m.koanf, nil
}

func (m *Module) provideReloadNotifier(_ do.Injector) (ReloadNotifier, error) { //nolint:ireturn
	return m, nil
}

// Koanf returns the koanf instance (thread-safe for hot-reload).
func (m *Module) Koanf() *koanf.Koanf {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.koanf
}

// OnReload registers a callback that is invoked after config is successfully reloaded.
// Callbacks run under the module's write lock, so they must not call back into the config module.
func (m *Module) OnReload(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReload = append(m.onReload, fn)
}

func (m *Module) reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newKoanf := koanf.New(".")

	for _, cf := range m.configFiles {
		if err := newKoanf.Load(file.Provider(cf.path), cf.parser); err != nil {
			return oops.Wrapf(err, "failed to reload config file: %s", cf.path)
		}
	}

	if err := newKoanf.Load(env.Provider(m.config.EnvPrefix, ".", func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, m.config.EnvPrefix), "_", "."))
	}), nil); err != nil {
		return oops.Wrapf(err, "failed to reload env vars")
	}

	if m.flagSet != nil {
		if err := newKoanf.Load(posflag.Provider(m.flagSet, ".", newKoanf), nil); err != nil {
			return oops.Wrapf(err, "failed to reload CLI flags")
		}
	}

	m.koanf = newKoanf

	for _, fn := range m.onReload {
		fn()
	}

	return nil
}
