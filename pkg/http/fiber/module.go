package fiberserver

import (
	"context"
	"log/slog"
	"net"
	"net/netip"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	otelfiber "github.com/gofiber/contrib/v3/otel"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

// Module manages a Fiber HTTP server lifecycle.
type Module struct {
	config Config

	server   *fiber.App
	addrPort netip.AddrPort
	listener net.Listener

	runtimeContext context.Context //nolint:containedctx
}

// NewModule creates a new Fiber HTTP server module with the given options.
func NewModule(options ...Option) *Module {
	return &Module{config: NewConfig(options...)}
}

// Name returns the instance name.
func (m *Module) Name() string {
	return m.config.Name
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHTTP, "fiber", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	path := m.ConfigPath()
	if k.Exists(path) {
		return m.config.LoadFromKoanf(k, path)
	}
	return nil
}

// Init loads configuration, creates the Fiber app, and registers middleware and routes.
func (m *Module) Init(ctx context.Context) error {
	// Load config from koanf if available
	if k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx)); err == nil {
		if err := m.LoadConfig(k); err != nil {
			return oops.Wrapf(err, "failed to load config")
		}
	}

	app := fiber.New(m.config.ToFiberConfig())

	app.Hooks().OnPreStartupMessage(func(msgData *fiber.PreStartupMessageData) error {
		msgData.PreventDefault = true
		return nil
	})

	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	app.Use(otelfiber.Middleware())

	// Inject context
	app.Use(func(c fiber.Ctx) error {
		span := trace.SpanFromContext(c.Context())
		runtimeCtx := trace.ContextWithSpan(m.runtimeContext, span)
		c.SetContext(runtimeCtx)
		return c.Next()
	})

	for _, router := range m.config.Routers {
		router(app)
	}

	m.server = app

	addrPort, err := m.config.AddrPort()
	if err != nil {
		return oops.Wrapf(err, "failed to parse host address")
	}
	m.addrPort = addrPort

	return nil
}

// Start begins listening and serving HTTP requests.
func (m *Module) Start(ctx context.Context) error {
	m.runtimeContext = ctx

	if m.config.HealthPath != "" {
		h, err := do.Invoke[*health.Health](lakta.GetInjector(ctx))
		if err != nil {
			return oops.Wrapf(err, "failed to get health instance")
		}
		m.server.Get(m.config.HealthPath, adaptor.HTTPHandlerFunc(h.HandlerFunc))
	}

	var err error
	m.listener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", m.addrPort.String())
	if err != nil {
		return oops.Wrapf(err, "failed to listen on %s", m.addrPort)
	}

	slox.Info(ctx, "fiber http server started", slog.String("address", m.addrPort.String()))

	var wg errgroup.Group

	wg.Go(func() error {
		return oops.Wrapf(m.server.Listener(m.listener), "failed to start fiber http server")
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- wg.Wait()
	}()

	select {
	case <-ctx.Done():
		return m.Shutdown(ctx)
	case err := <-startDone:
		if err != nil {
			return err
		}
	}

	return nil
}

// Shutdown is a no-op; fiber handles its own shutdown via listener close.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

// fiber:context-methods migrated
