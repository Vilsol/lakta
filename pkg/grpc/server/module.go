package grpcserver

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
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
	"google.golang.org/grpc/codes"
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
	listener net.Listener
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
		runtimeCtx := trace.ContextWithSpan(m.RuntimeCtx(), span)
		return handler(runtimeCtx, req)
	}

	streamContextInjector := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		span := trace.SpanFromContext(ss.Context())
		runtimeCtx := trace.ContextWithSpan(m.RuntimeCtx(), span)
		return handler(srv, &contextServerStream{ServerStream: ss, ctx: runtimeCtx})
	}

	interceptorLogger := func() logging.Logger {
		return logging.LoggerFunc(func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
			slox.Log(ctx, slog.Level(lvl), msg, fields...)
		})
	}

	recoveryHandler := func(p any) error {
		return status.Errorf(codes.Unknown, "panic triggered: %v", p)
	}

	server := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			contextInjector,
			logging.UnaryServerInterceptor(interceptorLogger(), logging.WithLogOnEvents(logging.FinishCall)),
			recovery.UnaryServerInterceptor(recovery.WithRecoveryHandler(recoveryHandler)),
		),
		grpc.ChainStreamInterceptor(
			streamContextInjector,
			logging.StreamServerInterceptor(interceptorLogger(), logging.WithLogOnEvents(logging.FinishCall)),
			recovery.StreamServerInterceptor(recovery.WithRecoveryHandler(recoveryHandler)),
		),
	)

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

	var err error
	m.listener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", m.addrPort.String())
	if err != nil {
		return oops.Wrapf(err, "failed to listen on %s", m.addrPort)
	}

	slox.Info(ctx, "gRPC server started", slog.String("address", m.addrPort.String()))

	var wg errgroup.Group
	wg.Go(func() error {
		return oops.Wrapf(m.server.Serve(m.listener), "failed to serve gRPC server")
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

// Shutdown gracefully stops the gRPC server.
func (m *Module) Shutdown(_ context.Context) error {
	m.server.GracefulStop()
	return nil
}

// Addr returns the listener's network address, or nil if the server has not started yet.
func (m *Module) Addr() net.Addr {
	if m.listener == nil {
		return nil
	}
	return m.listener.Addr()
}

type contextServerStream struct {
	grpc.ServerStream

	ctx context.Context //nolint:containedctx
}

func (s *contextServerStream) Context() context.Context {
	return s.ctx
}
