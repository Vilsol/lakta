package fiberserver

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	otelfiber "github.com/gofiber/contrib/v3/otel"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

// Module manages a Fiber HTTP server lifecycle.
type Module struct {
	lakta.NamedBase
	lakta.SyncCtx

	config Config

	server   *fiber.App
	addrPort netip.AddrPort
	listener net.Listener
}

// NewModule creates a new Fiber HTTP server module with the given options.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHTTP, "fiber", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init loads configuration, creates the Fiber app, and registers middleware and routes.
func (m *Module) Init(ctx context.Context) error {
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
		runtimeCtx := trace.ContextWithSpan(m.RuntimeCtx(), span)
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
	if m.config.HealthPath != "" {
		h, err := lakta.Invoke[*health.Health](ctx)
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

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown is a no-op; fiber handles its own shutdown via listener close.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

// Addr returns the listener's network address, or nil if the server has not started yet.
func (m *Module) Addr() net.Addr {
	if m.listener == nil {
		return nil
	}
	return m.listener.Addr()
}

// fiber:context-methods migrated
