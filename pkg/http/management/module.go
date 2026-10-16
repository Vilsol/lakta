package management

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"sync"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

const readHeaderTimeout = 5 * time.Second

// Module serves /health, /ready and /metrics.
type Module struct {
	lakta.NamedBase

	config Config

	handler  http.Handler
	addrPort netip.AddrPort

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
}

// NewModule creates a new management server module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{NamedBase: lakta.NewNamedBase(cfg.Name), config: cfg}
}

// ConfigPath returns modules.http.management.<name>.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHTTP, "management", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error { return m.config.LoadFromKoanf(k, m.ConfigPath()) }

// Init builds the routes. /ready runs every check registered on *health.Health
// at request time, so checks added after Init (lazy clients) are included.
// Without *health.Health, /ready answers OK; without a registry, /metrics 404s.
func (m *Module) Init(ctx context.Context) error {
	injector := lakta.GetInjector(ctx)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	})

	if h, err := do.Invoke[*health.Health](injector); err == nil {
		mux.Handle("GET /ready", h.Handler())
	} else {
		mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"OK"}`))
		})
	}

	if reg, err := do.Invoke[*prometheus.Registry](injector); err == nil {
		mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	}

	addrPort, err := m.config.AddrPort()
	if err != nil {
		return oops.Wrapf(err, "failed to parse host address")
	}

	m.addrPort = addrPort
	m.handler = mux

	return nil
}

// DescribeConfig returns the config loaded from k without changing the module.
func (m *Module) DescribeConfig(k *koanf.Koanf) (any, error) {
	cfg := m.config
	err := cfg.LoadFromKoanf(k, m.ConfigPath())
	return cfg, err
}

// Start listens and serves until ctx is done.
func (m *Module) Start(ctx context.Context) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", m.addrPort.String())
	if err != nil {
		return oops.Wrapf(err, "failed to listen on %s", m.addrPort)
	}

	server := &http.Server{
		Handler:           m.handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	m.mu.Lock()
	m.listener = listener
	m.server = server
	m.mu.Unlock()

	slox.Info(ctx, "management server started", slog.String("address", m.addrPort.String()))

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-serveErr:
		if stderrors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return oops.Wrapf(err, "failed to serve management server")
	}
}

// Shutdown stops the server.
func (m *Module) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	srv := m.server
	m.mu.Unlock()

	if srv == nil {
		return nil
	}

	return oops.Wrapf(srv.Shutdown(ctx), "failed to shut down management server")
}

// Dependencies declares the optional health container, metrics registry and
// config this module reads in Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[*health.Health](),
		reflect.TypeFor[*prometheus.Registry](),
	}
}

// Addr returns the listener's network address, or nil before Start.
func (m *Module) Addr() net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listener == nil {
		return nil
	}
	return m.listener.Addr()
}
