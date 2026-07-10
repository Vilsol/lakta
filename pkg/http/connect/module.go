package connect

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"sync"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"golang.org/x/sync/errgroup"
)

// Module manages a Connect-RPC (net/http) server lifecycle. One handler serves
// Connect, gRPC-Web, and gRPC on one port; cleartext HTTP/2 (h2c) lets gRPC
// clients connect without TLS.
type Module struct {
	lakta.NamedBase
	lakta.SyncCtx

	config Config

	handler  http.Handler
	server   *http.Server
	addrPort netip.AddrPort

	mu       sync.Mutex
	listener net.Listener
}

// NewModule creates a new Connect-RPC server module with the given options.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{NamedBase: lakta.NewNamedBase(cfg.Name), config: cfg}
}

// ConfigPath returns modules.http.connect.<name>.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHTTP, "connect", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error { return m.config.LoadFromKoanf(k, m.ConfigPath()) }

// Init builds the ServeMux, mounts WithHandler entries as-is and passes the
// shared []connect.HandlerOption chain into each WithService registrar.
func (m *Module) Init(ctx context.Context) error {
	mux := http.NewServeMux()
	opts := m.config.Interceptors()

	for path, h := range m.config.Handlers {
		mux.Handle(path, h)
	}
	for _, reg := range m.config.ServiceRegistrars {
		path, h := reg(ctx, opts)
		mux.Handle(path, h)
	}

	addrPort, err := m.config.AddrPort()
	if err != nil {
		return oops.Wrapf(err, "failed to parse host address")
	}
	m.addrPort = addrPort
	m.handler = mux

	return nil
}

// Start listens and serves; races Serve against ctx.Done() (copied from grpc/server).
// When H2C is set and TLS is not, the server advertises cleartext HTTP/2 so gRPC
// clients connect without TLS (stdlib replacement for the deprecated x/net/h2c).
func (m *Module) Start(ctx context.Context) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", m.addrPort.String())
	if err != nil {
		return oops.Wrapf(err, "failed to listen on %s", m.addrPort)
	}

	tlsConfig, err := m.config.ServerTLSConfig()
	if err != nil {
		return oops.Wrapf(err, "failed to build server TLS config")
	}

	server := &http.Server{
		Handler:           m.handler,
		ReadTimeout:       m.config.ReadTimeout,
		ReadHeaderTimeout: m.config.ReadHeaderTimeout,
		TLSConfig:         tlsConfig,
	}
	if m.config.H2C && !m.config.TLS.Enabled() {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetUnencryptedHTTP2(true)
		server.Protocols = protocols
	}

	m.mu.Lock()
	m.listener = listener
	m.server = server
	m.mu.Unlock()

	slox.Info(ctx, "connect server started", slog.String("address", m.addrPort.String()))

	var wg errgroup.Group
	wg.Go(func() error {
		var serveErr error
		if tlsConfig != nil {
			serveErr = server.ServeTLS(listener, "", "")
		} else {
			serveErr = server.Serve(listener)
		}
		if stderrors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return oops.Wrapf(serveErr, "failed to serve connect server")
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- wg.Wait()
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-startDone:
		return err
	}
}

// Shutdown drains in-flight requests via http.Server.Shutdown raced against the
// runtime's 30s deadline (net/http analogue of grpc GracefulStop).
func (m *Module) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	srv := m.server
	m.mu.Unlock()

	if srv == nil {
		return nil
	}

	return oops.Wrapf(srv.Shutdown(ctx), "failed to shut down connect server")
}

// Dependencies declares the optional *koanf.Koanf this module needs before Init
// (copied from grpc/server). No required deps; Provides nothing.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{reflect.TypeFor[*koanf.Koanf]()}
}

// Addr returns the listener's network address, or nil before Start (tests bind port: 0).
func (m *Module) Addr() net.Addr {
	m.mu.Lock()
	listener := m.listener
	m.mu.Unlock()

	if listener == nil {
		return nil
	}
	return listener.Addr()
}
