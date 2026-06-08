package grpcserver

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

// injectRuntimeCtx sets the unexported SyncCtx.ctx field via reflection so the
// stream interceptors (which call m.RuntimeCtx()) don't panic in tests.
func injectRuntimeCtx(m *Module, ctx context.Context) {
	rv := reflect.ValueOf(&m.SyncCtx).Elem()
	f := rv.FieldByName("ctx")
	// Use reflect unsafe trick: get a pointer to the unexported field.
	reflect.NewAt(f.Type(), f.Addr().UnsafePointer()).Elem().Set(reflect.ValueOf(ctx))
}

func TestShutdown_ForcesStopOnDeadline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewModule(WithHost("127.0.0.1"), WithPort(0))
	injectRuntimeCtx(m, ctx) // required: stream interceptors call m.RuntimeCtx() lazily
	testza.AssertNoError(t, m.Init(ctx))

	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	testza.AssertNoError(t, err)
	defer func() { _ = lis.Close() }()

	hs := grpchealth.NewServer()
	healthpb.RegisterHealthServer(m.server, hs)
	go func() { _ = m.server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNoError(t, err)
	defer func() { _ = conn.Close() }()

	// Open a long-lived Watch stream so GracefulStop would block until it ends.
	stream, err := healthpb.NewHealthClient(conn).Watch(ctx, &healthpb.HealthCheckRequest{})
	testza.AssertNoError(t, err)
	defer func() { _ = stream.CloseSend() }()
	_, _ = stream.Recv() // receive initial status; stream stays open

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	testza.AssertNoError(t, m.Shutdown(shutdownCtx))
	testza.AssertTrue(t, time.Since(start) < 2*time.Second, "Shutdown must force-stop within the deadline")
}
