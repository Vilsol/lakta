package grpcserver_test

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/health"
	"github.com/Vilsol/lakta/pkg/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestGRPCServerModule_Listens(t *testing.T) {
	t.Parallel()

	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)
	testza.AssertNotNil(t, addr)
	testza.AssertNotEqual(t, "", addr.String())
}

func TestGRPCServerModule_HealthCheck_NoHealth(t *testing.T) {
	t.Parallel()

	// Without a health.Module in DI, the health endpoint returns NOT_SERVING.
	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithHealthCheck(true),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)

	conn, err := grpc.NewClient(addr.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	testza.AssertNil(t, err)
	testza.AssertEqual(t, healthpb.HealthCheckResponse_NOT_SERVING, resp.GetStatus())
}

func TestGRPCServerModule_HealthCheck_WithHealth(t *testing.T) {
	t.Parallel()

	// With a health.Module in DI, the health endpoint returns SERVING.
	healthM := health.NewModule()
	serverM := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithHealthCheck(true),
	)

	testkit.NewRuntimeHarness(t, healthM, serverM)

	addr := testkit.WaitForAddr(t, serverM)

	conn, err := grpc.NewClient(addr.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	resp, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{})
	testza.AssertNil(t, err)
	testza.AssertEqual(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())
}

func TestGRPCServerModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.grpc.server.default", grpcserver.NewModule().ConfigPath())
}

func TestGRPCServerModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", grpcserver.NewModule().Name())
	testza.AssertEqual(t, "custom", grpcserver.NewModule(grpcserver.WithName("custom")).Name())
}

func TestGRPCServerModule_AddrNilBeforeStart(t *testing.T) {
	t.Parallel()

	m := grpcserver.NewModule()
	testza.AssertNil(t, m.Addr())
}
