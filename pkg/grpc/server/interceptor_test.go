package grpcserver_test

import (
	"context"
	"sync"
	"testing"

	"github.com/MarvinJWendt/testza"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	errgrpc "github.com/Vilsol/lakta/pkg/errors/grpc"
	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/testkit"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// notFoundHealthServer returns a typed AppError so the errgrpc interceptor can
// render it end-to-end.
type notFoundHealthServer struct {
	healthpb.UnimplementedHealthServer
}

func (notFoundHealthServer) Check(context.Context, *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return nil, apperrors.NotFound("user not found").WithMeta("resource", "user")
}

// okHealthServer returns a normal SERVING response.
type okHealthServer struct {
	healthpb.UnimplementedHealthServer
}

func (okHealthServer) Check(context.Context, *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func dialServer(t *testing.T, m *grpcserver.Module) healthpb.HealthClient { //nolint:ireturn // grpc client is inherently an interface
	t.Helper()
	addr := testkit.WaitForAddr(t, m)
	conn, err := grpc.NewClient(addr.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return healthpb.NewHealthClient(conn)
}

func TestGRPCServer_ErrgrpcRendersAppError(t *testing.T) {
	t.Parallel()

	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithService(&healthpb.Health_ServiceDesc, notFoundHealthServer{}),
		grpcserver.WithUnaryInterceptor(errgrpc.UnaryServerInterceptor()),
	)
	testkit.NewRuntimeHarness(t, m)

	_, err := dialServer(t, m).Check(context.Background(), &healthpb.HealthCheckRequest{})
	st, ok := status.FromError(err)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, codes.NotFound, st.Code())
	testza.AssertEqual(t, "user not found", st.Message())

	var info *errdetails.ErrorInfo
	for _, d := range st.Details() {
		if got, isInfo := d.(*errdetails.ErrorInfo); isInfo {
			info = got
		}
	}
	testza.AssertNotNil(t, info)
	testza.AssertEqual(t, "NOT_FOUND", info.GetReason())
	testza.AssertEqual(t, "user", info.GetMetadata()["resource"])
}

func TestGRPCServer_UnaryInterceptorOrder(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var order []string
	probe := func(name string) grpc.UnaryServerInterceptor {
		return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return handler(ctx, req)
		}
	}

	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithService(&healthpb.Health_ServiceDesc, okHealthServer{}),
		grpcserver.WithUnaryInterceptor(probe("first")),
		grpcserver.WithUnaryInterceptor(probe("second")),
	)
	testkit.NewRuntimeHarness(t, m)

	resp, err := dialServer(t, m).Check(context.Background(), &healthpb.HealthCheckRequest{})
	testza.AssertNil(t, err)
	testza.AssertEqual(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())

	mu.Lock()
	defer mu.Unlock()
	testza.AssertEqual(t, []string{"first", "second"}, order)
}

func TestGRPCServer_StreamInterceptorInjected(t *testing.T) {
	t.Parallel()

	var called bool
	var mu sync.Mutex
	probe := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		mu.Lock()
		called = true
		mu.Unlock()
		return handler(srv, ss)
	}

	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithService(&healthpb.Health_ServiceDesc, okHealthServer{}),
		grpcserver.WithStreamInterceptor(probe),
	)
	testkit.NewRuntimeHarness(t, m)

	stream, err := dialServer(t, m).Watch(context.Background(), &healthpb.HealthCheckRequest{})
	testza.AssertNil(t, err)
	_, _ = stream.Recv() // Watch is Unimplemented; we only care that the probe ran.

	mu.Lock()
	defer mu.Unlock()
	testza.AssertTrue(t, called)
}
