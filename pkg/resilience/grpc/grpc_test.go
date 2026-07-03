package grpc_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	resgrpc "github.com/Vilsol/lakta/pkg/resilience/grpc"
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/samber/do/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type flakyHealthServer struct {
	grpchealth.UnimplementedHealthServer

	calls atomic.Int32
}

func (s *flakyHealthServer) Check(_ context.Context, _ *grpchealth.HealthCheckRequest) (*grpchealth.HealthCheckResponse, error) {
	if s.calls.Add(1) == 1 {
		return nil, status.Error(codes.Unavailable, "first call always fails") //nolint:wrapcheck // raw gRPC status is the test fixture
	}
	return &grpchealth.HealthCheckResponse{Status: grpchealth.HealthCheckResponse_SERVING}, nil
}

func newRegistry(t *testing.T, options ...policy.Option) *policy.Registry {
	t.Helper()
	h := testkit.NewHarness(t)
	m := policy.NewModule(options...)
	testza.AssertNoError(t, m.Init(h.Ctx()))

	reg, err := do.Invoke[*policy.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	return reg
}

func TestNewUnaryClientInterceptor_RetriesFailedCalls(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t, policy.WithPolicy("upstream",
		retrypolicy.NewBuilder[any]().WithMaxAttempts(2).Build(),
	))

	srv := &flakyHealthServer{}
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	grpchealth.RegisterHealthServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	interceptor, err := resgrpc.NewUnaryClientInterceptor(reg, "upstream")
	testza.AssertNoError(t, err)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptor),
	)
	testza.AssertNoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := grpchealth.NewHealthClient(conn).Check(t.Context(), &grpchealth.HealthCheckRequest{})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, grpchealth.HealthCheckResponse_SERVING, resp.GetStatus())
	testza.AssertEqual(t, int32(2), srv.calls.Load())
}

func TestNewUnaryClientInterceptor_UnknownPolicyErrors(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t)

	_, err := resgrpc.NewUnaryClientInterceptor(reg, "missing")
	testza.AssertNotNil(t, err)
}

func TestNewUnaryServerInterceptor_UnknownPolicyErrors(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t)

	_, err := resgrpc.NewUnaryServerInterceptor(reg, "missing")
	testza.AssertNotNil(t, err)
}

func TestNewServerInHandle_UnknownPolicyErrors(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t)

	_, err := resgrpc.NewServerInHandle(reg, "missing")
	testza.AssertNotNil(t, err)
}
