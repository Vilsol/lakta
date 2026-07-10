package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	resgrpc "github.com/Vilsol/lakta/pkg/resilience/grpc"
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

//nolint:ireturn // returns the generated gRPC client interface, which is the usable test handle
func dialWithServerInterceptor(t *testing.T, reg *policy.Registry, name string) grpchealth.HealthClient {
	t.Helper()

	interceptor, err := resgrpc.NewUnaryServerInterceptor(reg, name)
	testza.AssertNoError(t, err)

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(interceptor))
	grpchealth.RegisterHealthServer(server, &okHealthServer{})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	testza.AssertNoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return grpchealth.NewHealthClient(conn)
}

type okHealthServer struct {
	grpchealth.UnimplementedHealthServer
}

func (s *okHealthServer) Check(_ context.Context, _ *grpchealth.HealthCheckRequest) (*grpchealth.HealthCheckResponse, error) {
	return &grpchealth.HealthCheckResponse{Status: grpchealth.HealthCheckResponse_SERVING}, nil
}

func TestServerInterceptor_BulkheadOverloadToUnavailable(t *testing.T) {
	t.Parallel()

	bh := bulkhead.NewBuilder[any](1).Build()
	testza.AssertNoError(t, bh.AcquirePermit(context.Background())) // saturate

	reg := newRegistry(t, policy.WithPolicy("api", bh))
	client := dialWithServerInterceptor(t, reg, "api")

	_, err := client.Check(t.Context(), &grpchealth.HealthCheckRequest{})
	testza.AssertEqual(t, codes.Unavailable, status.Code(err))
}

func TestServerInterceptor_RateLimitToResourceExhausted(t *testing.T) {
	t.Parallel()

	rl := ratelimiter.NewBursty[any](1, time.Minute)
	reg := newRegistry(t, policy.WithPolicy("api", rl))
	client := dialWithServerInterceptor(t, reg, "api")

	_, err := client.Check(t.Context(), &grpchealth.HealthCheckRequest{})
	testza.AssertNoError(t, err) // first call consumes the only permit

	_, err = client.Check(t.Context(), &grpchealth.HealthCheckRequest{})
	testza.AssertEqual(t, codes.ResourceExhausted, status.Code(err))
}

func TestServerInterceptor_OrdinaryErrorUnchanged(t *testing.T) {
	t.Parallel()

	rl := ratelimiter.NewBursty[any](100, time.Minute)
	reg := newRegistry(t, policy.WithPolicy("api", rl))

	interceptor, err := resgrpc.NewUnaryServerInterceptor(reg, "api")
	testza.AssertNoError(t, err)

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(interceptor))
	grpchealth.RegisterHealthServer(server, &failHealthServer{})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	testza.AssertNoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, err = grpchealth.NewHealthClient(conn).Check(t.Context(), &grpchealth.HealthCheckRequest{})
	testza.AssertEqual(t, codes.FailedPrecondition, status.Code(err))
}

type failHealthServer struct {
	grpchealth.UnimplementedHealthServer
}

func (s *failHealthServer) Check(_ context.Context, _ *grpchealth.HealthCheckRequest) (*grpchealth.HealthCheckResponse, error) {
	return nil, status.Error(codes.FailedPrecondition, "boom") //nolint:wrapcheck // raw gRPC status is the test fixture
}
