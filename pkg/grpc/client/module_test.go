package grpcclient_test

import (
	"context"
	"net"
	"testing"

	"github.com/MarvinJWendt/testza"
	grpcclient "github.com/Vilsol/lakta/pkg/grpc/client"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/samber/do/v2"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// testHealthServer is a minimal gRPC health server for use in tests.
type testHealthServer struct {
	healthpb.UnimplementedHealthServer
}

func (s *testHealthServer) Check(_ context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

// startTestGRPCServer starts a raw gRPC server with the health service registered.
// Returns the server address; the server is stopped on test cleanup.
func startTestGRPCServer(t *testing.T) string {
	t.Helper()

	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	testza.AssertNil(t, err)

	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, &testHealthServer{})

	go func() { _ = srv.Serve(lis) }()

	t.Cleanup(func() {
		srv.GracefulStop()
	})

	return lis.Addr().String()
}

func TestGRPCClientModule_Init(t *testing.T) {
	t.Parallel()

	addr := startTestGRPCServer(t)
	h := testkit.NewHarness(t)
	m := grpcclient.NewModule(
		grpcclient.WithTarget(addr),
		grpcclient.WithInsecure(true),
	)

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestGRPCClientModule_WithClient_RegistersInDI(t *testing.T) {
	t.Parallel()

	addr := startTestGRPCServer(t)
	h := testkit.NewHarness(t)
	m := grpcclient.NewModule(
		grpcclient.WithTarget(addr),
		grpcclient.WithInsecure(true),
		grpcclient.WithClient(healthpb.NewHealthClient),
	)

	testza.AssertNil(t, m.Init(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	client, err := do.Invoke[healthpb.HealthClient](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, client)
}

func TestGRPCClientModule_MakesRPCCall(t *testing.T) {
	t.Parallel()

	addr := startTestGRPCServer(t)
	h := testkit.NewHarness(t)
	m := grpcclient.NewModule(
		grpcclient.WithTarget(addr),
		grpcclient.WithInsecure(true),
		grpcclient.WithClient(healthpb.NewHealthClient),
	)

	testza.AssertNil(t, m.Init(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	client, err := do.Invoke[healthpb.HealthClient](h.Injector())
	testza.AssertNil(t, err)

	resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{})
	testza.AssertNil(t, err)
	testza.AssertEqual(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())
}

func TestGRPCClientModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.grpc.client.default", grpcclient.NewModule().ConfigPath())
}

func TestGRPCClientModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", grpcclient.NewModule().Name())
	testza.AssertEqual(t, "custom", grpcclient.NewModule(grpcclient.WithName("custom")).Name())
}
