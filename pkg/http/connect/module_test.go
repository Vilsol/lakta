package connect_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/MarvinJWendt/testza"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	testv1 "github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1"
	"github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1/testv1connect"
	"github.com/Vilsol/lakta/pkg/testkit"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// echoServer echoes the request message back.
type echoServer struct {
	prefix string
}

func (e echoServer) Echo(_ context.Context, req *connect.Request[testv1.EchoRequest]) (*connect.Response[testv1.EchoResponse], error) {
	return connect.NewResponse(&testv1.EchoResponse{Message: e.prefix + req.Msg.GetMessage()}), nil
}

func h2cClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

func echoModule(t *testing.T, prefix string) *connectmod.Module {
	t.Helper()
	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, opts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewEchoServiceHandler(echoServer{prefix: prefix}, opts...)
		}),
	)
	testkit.NewRuntimeHarness(t, m)
	return m
}

// TestConnectModule_ThreeProtocolsOnOnePort is the flagship: one handler answers
// both a Connect JSON POST and a stock grpc-go client on the same port.
func TestConnectModule_ThreeProtocolsOnOnePort(t *testing.T) {
	t.Parallel()

	m := echoModule(t, "")
	addr := testkit.WaitForAddr(t, m).String()

	// (a) Connect unary over plain HTTP with JSON codec.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://"+addr+"/test.v1.EchoService/Echo", strings.NewReader(`{"message":"hi"}`))
	testza.AssertNil(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	testza.AssertContains(t, string(body[:n]), `"hi"`)

	// (b) Stock grpc-go client dialing the identical port (insecure/h2c).
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	var out testv1.EchoResponse
	err = conn.Invoke(context.Background(), "/test.v1.EchoService/Echo", &testv1.EchoRequest{Message: "hi"}, &out)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "hi", out.GetMessage())
}

// TestConnectModule_H2CUpgrade asserts a cleartext HTTP/2 client reaches the
// handler and the negotiated protocol is HTTP/2.
func TestConnectModule_H2CUpgrade(t *testing.T) {
	t.Parallel()

	m := echoModule(t, "")
	addr := testkit.WaitForAddr(t, m).String()

	client := h2cClient()
	t.Cleanup(client.CloseIdleConnections)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://"+addr+"/test.v1.EchoService/Echo", strings.NewReader(`{"message":"hi"}`))
	testza.AssertNil(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
	testza.AssertEqual(t, 2, resp.ProtoMajor)
}

// TestConnectModule_GracefulDrain asserts Shutdown lets an in-flight request
// finish while refusing new connections.
func TestConnectModule_GracefulDrain(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, opts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewEchoServiceHandler(slowEcho{entered: entered, release: release}, opts...)
		}),
	)

	ctx := context.Background()
	testza.AssertNil(t, m.Init(ctx))
	go func() { _ = m.Start(ctx) }()
	addr := testkit.WaitForAddr(t, m).String()

	inflight := make(chan error, 1)
	go func() {
		client := testv1connect.NewEchoServiceClient(h2cClient(), "http://"+addr)
		_, err := client.Echo(context.Background(), connect.NewRequest(&testv1.EchoRequest{Message: "slow"}))
		inflight <- err
	}()

	<-entered // handler is now in-flight

	shutDone := make(chan error, 1)
	go func() { shutDone <- m.Shutdown(context.Background()) }()

	// The listener is closed by Shutdown; a fresh connection is refused.
	time.Sleep(100 * time.Millisecond)
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer dialCancel()
	_, dialErr := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	testza.AssertNotNil(t, dialErr)

	// Release the in-flight handler; it must complete cleanly and Shutdown returns.
	close(release)
	testza.AssertNil(t, <-inflight)
	testza.AssertNil(t, <-shutDone)
}

type slowEcho struct {
	entered chan struct{}
	release chan struct{}
}

func (s slowEcho) Echo(_ context.Context, req *connect.Request[testv1.EchoRequest]) (*connect.Response[testv1.EchoResponse], error) {
	close(s.entered)
	<-s.release
	return connect.NewResponse(&testv1.EchoResponse{Message: req.Msg.GetMessage()}), nil
}

func TestConnectModule_AddrNilBeforeStart(t *testing.T) {
	t.Parallel()
	testza.AssertNil(t, connectmod.NewModule().Addr())
}

func TestConnectModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.http.connect.default", connectmod.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.http.connect.custom", connectmod.NewModule(connectmod.WithName("custom")).ConfigPath())
}

func TestConnectModule_Name(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "default", connectmod.NewModule().Name())
	testza.AssertEqual(t, "custom", connectmod.NewModule(connectmod.WithName("custom")).Name())
}

func TestConnectModule_Defaults(t *testing.T) {
	t.Parallel()
	c := connectmod.NewDefaultConfig()
	testza.AssertTrue(t, c.H2C)
	testza.AssertEqual(t, 10*time.Second, c.ReadHeaderTimeout)
	testza.AssertEqual(t, time.Duration(0), c.ReadTimeout)
}
