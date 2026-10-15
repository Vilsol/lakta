package connect

import (
	"context"
	"crypto/tls"
	stderrors "errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"sync"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
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

	handler   http.Handler
	tlsConfig *tls.Config
	server    *http.Server
	addrPort  netip.AddrPort

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

// Init builds the interceptor slots, mounts WithHandler entries as-is, passes
// the shared []connect.HandlerOption into each WithService registrar, runs the
// mounted-service hooks and wraps the mux in the limits and HTTP middlewares.
func (m *Module) Init(ctx context.Context) error {
	mux := http.NewServeMux()

	opts, err := m.config.Interceptors(ctx)
	if err != nil {
		return oops.Wrapf(err, "failed to build interceptors")
	}

	paths := make([]string, 0, len(m.config.Handlers)+len(m.config.ServiceRegistrars))
	for path, h := range m.config.Handlers {
		mux.Handle(path, h)
		paths = append(paths, path)
	}
	for _, reg := range m.config.ServiceRegistrars {
		path, h := reg(ctx, opts)
		mux.Handle(path, h)
		paths = append(paths, path)
	}

	mounted := resolveMounted(paths)
	for _, hook := range m.config.MountedHooks {
		if err := hook(ctx, mounted); err != nil {
			return oops.Wrapf(err, "mounted-service hook failed")
		}
	}

	handler := m.config.limitsMiddleware(procedures(mounted), mux)
	for _, mw := range slices.Backward(m.config.HTTPMiddlewares) {
		handler = mw(handler)
	}
	handler = m.config.peerMiddleware(handler)

	if err := m.checkProduction(ctx); err != nil {
		return err
	}

	if m.config.TLSProvider != nil {
		tlsConfig, terr := m.config.TLSProvider(ctx)
		if terr != nil {
			return oops.Wrapf(terr, "failed to build server TLS config")
		}
		m.tlsConfig = tlsConfig
	}

	addrPort, err := m.config.AddrPort()
	if err != nil {
		return oops.Wrapf(err, "failed to parse host address")
	}
	m.addrPort = addrPort
	m.handler = handler

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

	tlsConfig := m.tlsConfig
	if tlsConfig == nil {
		tlsConfig, err = m.config.ServerTLSConfig()
		if err != nil {
			return oops.Wrapf(err, "failed to build server TLS config")
		}
	}

	// Handlers see the runtime ctx values (injector, logger) but not its
	// cancellation, which would abort in-flight requests before the drain.
	baseCtx := context.WithoutCancel(ctx)
	server := &http.Server{
		Handler:           m.handler,
		ReadHeaderTimeout: m.config.ReadHeaderTimeout,
		MaxHeaderBytes:    m.config.MaxHeaderBytes,
		TLSConfig:         tlsConfig,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ErrorLog:          newHandshakeLogger(baseCtx),
	}
	if m.config.H2C && tlsConfig == nil {
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

// checkProduction enforces RequirePeerAuth under a production profile: mTLS
// must be configured and the caller allow-list must not be empty.
func (m *Module) checkProduction(ctx context.Context) error {
	if !m.config.RequirePeerAuth {
		return nil
	}
	profile := config.Profile(os.Getenv("LAKTA_PROFILE"))
	if p, err := do.Invoke[config.Profile](lakta.GetInjector(ctx)); err == nil {
		profile = p
	}
	if !profile.IsProduction() {
		return nil
	}
	if m.config.TLSProvider == nil && !m.config.TLS.Enabled() {
		return oops.Errorf("connect: mTLS is required under production profile %q", profile)
	}
	if len(m.config.AllowedCallers) == 0 {
		return oops.Errorf("connect: allowed_callers must not be empty under production profile %q", profile)
	}
	return nil
}

// Dependencies declares the optional types this module reads before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{reflect.TypeFor[*koanf.Koanf](), reflect.TypeFor[config.Profile]()}
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
