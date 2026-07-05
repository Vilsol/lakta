// Package actuator mounts a Spring-Actuator-equivalent debug/introspection
// surface (modules, startup, routes, health, info, config, pprof, expvar,
// goroutine dump, DI graph, live log-level control) on its own loopback fiber
// listener. All dependencies are optional so it degrades gracefully: an absent
// source makes its endpoint return 501/empty rather than fail boot.
package actuator

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"reflect"
	"sync"

	"github.com/Vilsol/lakta/pkg/config"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/Vilsol/lakta/pkg/lakta"
	slogmod "github.com/Vilsol/lakta/pkg/logging/slog"
	"github.com/Vilsol/slox"
	"github.com/gofiber/fiber/v3"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

// Module is the actuator [SyncModule].
type Module struct {
	lakta.NamedBase
	lakta.SyncCtx

	config Config

	app      *fiber.App // private instance, or nil when WithMount is used
	addrPort netip.AddrPort
	mu       sync.Mutex
	listener net.Listener

	redactor *Redactor
	injector do.Injector

	// Optional dependencies resolved during Init; nil when the source is absent.
	runtimeInfo     *lakta.RuntimeInfo
	health          *health.Health
	koanf           *koanf.Koanf
	routes          *fiberserver.RoutesRegistry
	levelController slogmod.LevelController
	configModule    *config.Module
}

// NewModule creates a new actuator module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns modules.debug.actuator.<name>.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryDebug, "actuator", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init resolves optional sources from DI, builds the private *fiber.App (or the
// WithMount target), registers all endpoint handlers under BasePath, and applies
// the fail-closed security model. No-op when Enabled is false.
func (m *Module) Init(ctx context.Context) error {
	if !m.config.Enabled {
		return nil
	}

	redactor, err := m.config.SecretRedactor()
	if err != nil {
		return oops.Wrapf(err, "failed to build secret redactor")
	}
	m.redactor = redactor

	m.injector = lakta.GetInjector(ctx)
	m.runtimeInfo, _ = lakta.Invoke[*lakta.RuntimeInfo](ctx)
	m.health, _ = lakta.Invoke[*health.Health](ctx)
	m.koanf, _ = lakta.Invoke[*koanf.Koanf](ctx)
	m.levelController, _ = lakta.Invoke[slogmod.LevelController](ctx)
	m.configModule, _ = lakta.Invoke[*config.Module](ctx)

	// Provide-if-absent the shared routes registry so ordering vs the fiber
	// modules does not matter (see pkg/http/fiber routes.go).
	reg, regErr := lakta.Invoke[*fiberserver.RoutesRegistry](ctx)
	if regErr != nil {
		reg = fiberserver.NewRoutesRegistry()
		lakta.ProvideValue(ctx, reg)
	}
	m.routes = reg

	if secErr := m.applySecurity(ctx); secErr != nil {
		return secErr
	}

	if m.config.Mount != nil {
		m.registerHandlers(m.config.Mount.Group(m.config.BasePath))
		return nil
	}

	app := fiber.New()
	app.Hooks().OnPreStartupMessage(func(msgData *fiber.PreStartupMessageData) error {
		msgData.PreventDefault = true
		return nil
	})
	m.registerHandlers(app.Group(m.config.BasePath))
	m.app = app

	addrPort, err := m.config.addrPort()
	if err != nil {
		return oops.Wrapf(err, "failed to parse actuator address")
	}
	m.addrPort = addrPort

	return nil
}

// Start listens on Host:Port with graceful shutdown, mirroring pkg/http/fiber
// (incl. OnPreStartupMessage suppression). No-op when Enabled is false or when
// WithMount delegated serving to the host app.
func (m *Module) Start(ctx context.Context) error {
	if !m.config.Enabled || m.app == nil {
		return nil
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", m.addrPort.String())
	if err != nil {
		return oops.Wrapf(err, "failed to listen on %s", m.addrPort)
	}

	m.mu.Lock()
	m.listener = listener
	m.mu.Unlock()

	slox.Info(ctx, "actuator server started", slog.String("address", m.addrPort.String()))

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- m.app.Listener(listener)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-serveErr:
		return oops.Wrapf(err, "actuator server failed")
	}
}

// Shutdown drains the private listener.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.app == nil {
		return nil
	}
	return oops.Wrapf(m.app.ShutdownWithContext(ctx), "failed to shutdown actuator server")
}

// Dependencies declares no required deps; all optional so the module degrades to
// 501/empty when a source is absent.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[*lakta.RuntimeInfo](),
		reflect.TypeFor[*health.Health](),
		reflect.TypeFor[slogmod.LevelController](),
		reflect.TypeFor[*config.Module](),
		reflect.TypeFor[*fiberserver.RoutesRegistry](),
	}
}

// Addr returns the listener's network address, or nil before Start.
func (m *Module) Addr() net.Addr {
	m.mu.Lock()
	listener := m.listener
	m.mu.Unlock()

	if listener == nil {
		return nil
	}
	return listener.Addr()
}
