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

func TestStart_DoesNotStopServerOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewModule(WithHost("127.0.0.1"), WithPort(0))
	injectRuntimeCtx(m, ctx)
	testza.AssertNoError(t, m.Init(ctx))

	hs := grpchealth.NewServer()
	healthpb.RegisterHealthServer(m.server, hs)

	startCtx, cancelStart := context.WithCancel(ctx)
	startErr := make(chan error, 1)
	go func() {
		startErr <- m.Start(startCtx)
	}()

	// Poll until server is listening.
	var addr net.Addr
	for i := range 50 {
		addr = m.Addr()
		if addr != nil {
			break
		}
		if i == 49 {
			t.Fatal("server did not start listening within timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancelStart()

	select {
	case err := <-startErr:
		testza.AssertNil(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}

	// Server must still be serving — a fresh request must succeed.
	conn, err := grpc.NewClient(addr.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNoError(t, err)
	defer func() { _ = conn.Close() }()

	checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
	defer checkCancel()
	_, err = healthpb.NewHealthClient(conn).Check(checkCtx, &healthpb.HealthCheckRequest{})
	testza.AssertNoError(t, err)

	testza.AssertNoError(t, m.Shutdown(context.Background()))
}

func TestKeepaliveDefaults(t *testing.T) {
	t.Parallel()

	c := NewConfig()

	sp := c.KeepaliveServerParameters()
	testza.AssertEqual(t, 5*time.Minute, sp.MaxConnectionIdle)
	testza.AssertEqual(t, 2*time.Hour, sp.Time)
	testza.AssertEqual(t, 20*time.Second, sp.Timeout)

	ep := c.KeepaliveEnforcementPolicy()
	testza.AssertEqual(t, 30*time.Second, ep.MinTime)
	testza.AssertTrue(t, ep.PermitWithoutStream)
}
