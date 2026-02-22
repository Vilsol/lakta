package slog

import (
	"context"
	"log/slog"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	slogotel "github.com/remychantenay/slog-otel"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// Module configures and provides a slog.Logger with stack rewriting and per-package level filtering.
type Module struct {
	lakta.NamedBase

	logger       *slog.Logger
	runtimeFrame runtime.Frame
	config       Config
	filter       *levelFilter
}

const skippedFrames = 2

// NewModule creates a new slog module, capturing the caller's runtime frame.
func NewModule(options ...Option) *Module {
	// Resolve caller
	var pcs [32]uintptr
	runtime.Callers(skippedFrames, pcs[:])
	fs := runtime.CallersFrames(pcs[:])
	f, _ := fs.Next()

	// Bubble up to main
	for f.Function != "main.main" {
		next, more := fs.Next()
		if !more {
			break
		}
		f = next
	}

	cfg := NewConfig(options...)
	return &Module{
		NamedBase:    lakta.NewNamedBase(cfg.Name),
		runtimeFrame: f,
		config:       cfg,
	}
}

const loggingPath = "logging"

// Init creates the slog.Logger with stack rewriting, level filtering, and registers it in DI.
func (m *Module) Init(ctx context.Context) error {
	injector := lakta.GetInjector(ctx)

	if k, err := do.Invoke[*koanf.Koanf](injector); err == nil && k != nil {
		if k.Exists(loggingPath) {
			if err := m.config.LoadFromKoanf(k, loggingPath); err != nil {
				return oops.Wrapf(err, "failed to load logging config")
			}
		}
	}

	m.config.ParseLevel()
	m.config.ParseLevels()

	m.validateLevelPrefixes(ctx)

	handler, err := do.Invoke[slog.Handler](injector)
	if err != nil {
		return oops.Wrapf(err, "failed to retrieve logger handler")
	}

	fanout := slog.NewMultiHandler(
		slogotel.New(handler),
		otelslog.NewHandler(m.config.Name),
	)

	m.filter = newLevelFilter(fanout, m.config.levelParsed, m.config.levelsParsed)

	m.logger = slog.New(
		newStackRewriter(ctx, m.filter, m.runtimeFrame),
	)

	lakta.ProvideValue(ctx, m.logger)

	slog.SetDefault(m.logger)

	return nil
}

func (m *Module) OnReload(k *koanf.Koanf) {
	cfg := NewDefaultConfig()
	if k.Exists(loggingPath) {
		if err := cfg.LoadFromKoanf(k, loggingPath); err != nil {
			slog.Error("failed to reload logging config", slog.Any("error", err))
			return
		}
	}

	cfg.ParseLevel()
	cfg.ParseLevels()

	m.config = cfg
	m.filter.Update(cfg.levelParsed, cfg.levelsParsed)

	slog.Info("logging levels reloaded")
}

// validateLevelPrefixes warns if configured package prefixes don't match any known module.
func (m *Module) validateLevelPrefixes(ctx context.Context) {
	if len(m.config.Levels) == 0 {
		return
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	modulePaths := make([]string, 0, len(buildInfo.Deps)+1)
	if buildInfo.Main.Path != "" {
		modulePaths = append(modulePaths, buildInfo.Main.Path)
	}
	for _, dep := range buildInfo.Deps {
		modulePaths = append(modulePaths, dep.Path)
	}

	for prefix := range m.config.Levels {
		if !prefixMatchesAnyModule(prefix, modulePaths) {
			slox.Warn(ctx, "configured log level prefix does not match any known module",
				slog.String("prefix", prefix),
			)
		}
	}
}

// prefixMatchesAnyModule returns true if the prefix is a prefix of any module path,
// or if any module path is a prefix of the configured prefix (the prefix is a sub-package).
func prefixMatchesAnyModule(prefix string, modulePaths []string) bool {
	for _, mod := range modulePaths {
		if strings.HasPrefix(prefix, mod) || strings.HasPrefix(mod, prefix) {
			return true
		}
	}
	return false
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*slog.Logger](),
	}
}

// Dependencies declares the types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return []reflect.Type{
			reflect.TypeFor[slog.Handler](),
		}, []reflect.Type{
			reflect.TypeFor[*koanf.Koanf](),
		}
}

// Shutdown is a no-op for this module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}
