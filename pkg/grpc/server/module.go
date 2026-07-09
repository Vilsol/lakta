package grpcserver

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"reflect"
	"sync"

	"github.com/Vilsol/lakta/pkg/config"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// Module manages a gRPC server lifecycle.
type Module struct {
	lakta.NamedBase
	lakta.SyncCtx

	config Config

	server   *grpc.Server
	addrPort netip.AddrPort

	mu          sync.Mutex
	listener    net.Listener
	serveCtx    context.Context //nolint:containedctx // cancelled on shutdown deadline to drain in-flight handlers
	cancelServe context.CancelFunc
}

// serveContext lazily derives a cancellable child of the runtime context the
// first time a handler runs. Cancelling it on the shutdown deadline unblocks
// in-flight handlers so the single GracefulStop drains: gRPC offers no safe
// hard Stop() while GracefulStop is in progress (grpc/grpc-go#8480).
func (m *Module) serveContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serveCtx == nil {
		m.serveCtx, m.cancelServe = context.WithCancel(m.RuntimeCtx())
	}

	return m.serveCtx
}

// NewModule creates a new gRPC server module with the given options.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryGRPC, "server", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init loads configuration and creates the gRPC server with interceptors.
func (m *Module) Init(ctx context.Context) error {
	contextInjector := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		span := trace.SpanFromContext(ctx)
		runtimeCtx := trace.ContextWithSpan(m.serveContext(), span)
		return handler(runtimeCtx, req)
	}

	streamContextInjector := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		span := trace.SpanFromContext(ss.Context())
		runtimeCtx := trace.ContextWithSpan(m.serveContext(), span)
		return handler(srv, &contextServerStream{ServerStream: ss, ctx: runtimeCtx})
	}

	interceptorLogger := func() logging.Logger {
		return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
			slox.Log(ctx, slog.Level(lvl), msg, fields...)
		})
	}

	recoveryHandler := func(ctx context.Context, p any) error {
		slox.Error(ctx, "recovered from panic in grpc handler", slog.Any("panic", p))
		// Recovery is outer of the appended errors interceptor, so it renders the
		// panic itself: build an INTERNAL AppError and map it to an opaque status.
		// The panic value stays in the log line above, never on the wire.
		appErr := apperrors.Internal("internal error")
		return status.New(appErr.GRPC, appErr.Message).Err()
	}

	creds, err := m.config.ServerCredentials()
	if err != nil {
		return oops.Wrapf(err, "failed to resolve server credentials")
	}

	serverOptions := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.KeepaliveParams(m.config.KeepaliveServerParameters()),
		grpc.KeepaliveEnforcementPolicy(m.config.KeepaliveEnforcementPolicy()),
		grpc.ChainUnaryInterceptor(append(
			[]grpc.UnaryServerInterceptor{
				contextInjector,
				logging.UnaryServerInterceptor(interceptorLogger(), logging.WithLogOnEvents(logging.FinishCall)),
				recovery.UnaryServerInterceptor(recovery.WithRecoveryHandlerContext(recoveryHandler)),
			},
			m.config.UnaryInterceptors...,
		)...),
		grpc.ChainStreamInterceptor(append(
			[]grpc.StreamServerInterceptor{
				streamContextInjector,
				logging.StreamServerInterceptor(interceptorLogger(), logging.WithLogOnEvents(logging.FinishCall)),
				recovery.StreamServerInterceptor(recovery.WithRecoveryHandlerContext(recoveryHandler)),
			},
			m.config.StreamInterceptors...,
		)...),
	}

	if creds != nil {
		serverOptions = append(serverOptions, grpc.Creds(creds))
	}

	server := grpc.NewServer(serverOptions...)

	for descriptor, service := range m.config.Services {
		server.RegisterService(descriptor, service)
	}

	m.server = server

	addrPort, err := m.config.AddrPort()
	if err != nil {
		return oops.Wrapf(err, "failed to parse host address")
	}
	m.addrPort = addrPort

	return nil
}

// Start begins listening and serving gRPC requests.
func (m *Module) Start(ctx context.Context) error {
	if m.config.HealthCheck {
		healthpb.RegisterHealthServer(m.server, newHealthServer(ctx))
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", m.addrPort.String())
	if err != nil {
		return oops.Wrapf(err, "failed to listen on %s", m.addrPort)
	}

	m.mu.Lock()
	m.listener = listener
	m.mu.Unlock()

	slox.Info(ctx, "gRPC server started", slog.String("address", m.addrPort.String()))

	var wg errgroup.Group
	wg.Go(func() error {
		return oops.Wrapf(m.server.Serve(listener), "failed to serve gRPC server")
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- wg.Wait()
	}()

	select {
	case <-ctx.Done():
		return nil
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

// Shutdown gracefully stops the gRPC server. If the context deadline is
// exceeded before in-flight RPCs drain, it cancels their handler contexts so
// they return and GracefulStop completes. It deliberately never calls Stop()
// concurrently with GracefulStop: gRPC funnels both into one internal routine
// guarded by a shared mutex, and calling them together deadlocks
// (grpc/grpc-go#8480, grpc/grpc-go#4584).
func (m *Module) Shutdown(ctx context.Context) error {
	if m.server == nil {
		return nil
	}

	stopped := make(chan struct{})
	go func() {
		m.server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		m.mu.Lock()
		cancel := m.cancelServe
		m.mu.Unlock()

		if cancel != nil {
			cancel()
		}

		<-stopped
	}

	return nil
}

// Addr returns the listener's network address, or nil if the server has not started yet.
func (m *Module) Addr() net.Addr {
	m.mu.Lock()
	listener := m.listener
	m.mu.Unlock()

	if listener == nil {
		return nil
	}
	return listener.Addr()
}

type contextServerStream struct {
	grpc.ServerStream

	ctx context.Context //nolint:containedctx
}

func (s *contextServerStream) Context() context.Context {
	return s.ctx
}
