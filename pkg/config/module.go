package config

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"github.com/spf13/pflag"
)

// IsConfigModule is a marker method to identify this module as the config module.
func (m *Module) IsConfigModule() {}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[ReloadNotifier](),
	}
}

// Module is the configuration module that loads and provides configuration.
type Module struct {
	config         Config
	koanf          *koanf.Koanf
	mu             sync.RWMutex
	configFiles    []configFile
	flagSet        *pflag.FlagSet
	onReload       []func(k *koanf.Koanf)
	onValidate     []func(k *koanf.Koanf) error
	watcherFactory func() (fileWatcher, error)
}

// NewModule creates a new config module.
func NewModule(options ...Option) *Module {
	return &Module{
		config:         NewConfig(options...),
		koanf:          koanf.New("."),
		watcherFactory: defaultWatcherFactory,
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

	lakta.ProvideValue(ctx, m.koanf)
	lakta.ProvideValue[ReloadNotifier](ctx, m)

	return nil
}

// envKeyTransform maps an environment variable name to a koanf path. A double
// underscore separates path segments; a single underscore is literal, so
// snake_case config keys survive intact:
//
//	LAKTA_MODULES__GRPC__SERVER__DEFAULT__PORT      -> modules.grpc.server.default.port
//	LAKTA_MODULES__DB__PGX__DEFAULT__MAX_OPEN_CONNS -> modules.db.pgx.default.max_open_conns
func envKeyTransform(prefix, s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, prefix), "__", "."))
}

func (m *Module) loadEnvVars() error {
	prefix := m.config.EnvPrefix
	err := m.koanf.Load(env.Provider(prefix, ".", func(s string) string {
		return envKeyTransform(prefix, s)
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
	m.flagSet.ParseErrorsAllowlist.UnknownFlags = true

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

	// Unknown flags end up in Args() — parse them as --key=value pairs
	for _, arg := range m.flagSet.Args() {
		if k, v, ok := parseFlag(arg); ok {
			if err := m.koanf.Set(k, v); err != nil {
				return oops.Wrapf(err, "failed to set CLI flag %q", k)
			}
		}
	}

	return nil
}

func parseFlag(arg string) (string, string, bool) {
	raw, ok := strings.CutPrefix(arg, "--")
	if !ok {
		return "", "", false
	}
	if k, v, ok := strings.Cut(raw, "="); ok {
		return k, v, true
	}
	return "", "", false
}

// Shutdown gracefully shuts down the config module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

// Koanf returns the koanf instance (thread-safe for hot-reload).
func (m *Module) Koanf() *koanf.Koanf {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.koanf
}

// OnReload registers a callback that is invoked after config is successfully reloaded.
// Callbacks run under the module's write lock, so they must not call back into the config module.
func (m *Module) OnReload(fn func(k *koanf.Koanf)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReload = append(m.onReload, fn)
}

// OnValidate registers a validator invoked on the candidate koanf before a
// reload is committed. A non-nil error aborts the reload.
// Validators run under the module's write lock, so they must not call back into the config module.
func (m *Module) OnValidate(fn func(k *koanf.Koanf) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onValidate = append(m.onValidate, fn)
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
		return envKeyTransform(m.config.EnvPrefix, s)
	}), nil); err != nil {
		return oops.Wrapf(err, "failed to reload env vars")
	}

	if m.flagSet != nil {
		if err := newKoanf.Load(posflag.Provider(m.flagSet, ".", newKoanf), nil); err != nil {
			return oops.Wrapf(err, "failed to reload CLI flags")
		}
	}

	for _, validate := range m.onValidate {
		if err := validate(newKoanf); err != nil {
			return oops.Wrapf(err, "config reload rejected by validator")
		}
	}

	m.koanf = newKoanf

	for _, fn := range m.onReload {
		m.safeCallback(fn, newKoanf)
	}

	return nil
}

// safeCallback runs a reload callback, isolating panics so one bad module does
// not crash the process or prevent other callbacks from running.
func (m *Module) safeCallback(fn func(k *koanf.Koanf), k *koanf.Koanf) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("config reload callback panicked", slog.Any("panic", r))
		}
	}()
	fn(k)
}
