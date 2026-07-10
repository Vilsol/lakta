package connect_test

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/MarvinJWendt/testza"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	testv1 "github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1"
	"github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1/testv1connect"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/samber/do/v2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// validateServer never runs its body — the validation interceptor rejects first.
type validateServer struct{}

func (validateServer) Check(_ context.Context, _ *connect.Request[testv1.CheckRequest]) (*connect.Response[testv1.CheckResponse], error) {
	return connect.NewResponse(&testv1.CheckResponse{}), nil
}

// TestConnectModule_ValidationParity asserts a protovalidate CEL violation
// renders CodeInvalidArgument with the same errdetails.BadRequest/ErrorInfo a
// gRPC client sees: reason is the rule id's final segment ("email").
func TestConnectModule_ValidationParity(t *testing.T) {
	t.Parallel()

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, opts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewValidateServiceHandler(validateServer{}, opts...)
		}),
	)
	testkit.NewRuntimeHarness(t, m)
	addr := testkit.WaitForAddr(t, m).String()

	client := testv1connect.NewValidateServiceClient(h2cClient(), "http://"+addr)
	_, err := client.Check(context.Background(), connect.NewRequest(&testv1.CheckRequest{Email: "not-an-email"}))
	testza.AssertNotNil(t, err)

	var ce *connect.Error
	testza.AssertTrue(t, stderrors.As(err, &ce))
	testza.AssertEqual(t, connect.CodeInvalidArgument, ce.Code())

	var br *errdetails.BadRequest
	var info *errdetails.ErrorInfo
	for _, d := range ce.Details() {
		msg, verr := d.Value()
		if verr != nil {
			continue
		}
		switch typed := msg.(type) {
		case *errdetails.BadRequest:
			br = typed
		case *errdetails.ErrorInfo:
			info = typed
		}
	}

	testza.AssertNotNil(t, info)
	testza.AssertEqual(t, "VALIDATION", info.GetReason())
	testza.AssertNotNil(t, br)
	testza.AssertEqual(t, 1, len(br.GetFieldViolations()))
	testza.AssertEqual(t, "email", br.GetFieldViolations()[0].GetField())
	testza.AssertEqual(t, "email", br.GetFieldViolations()[0].GetDescription())
}

// panicEcho panics to exercise the recovery interceptor.
type panicEcho struct{}

func (panicEcho) Echo(_ context.Context, _ *connect.Request[testv1.EchoRequest]) (*connect.Response[testv1.EchoResponse], error) {
	panic("handler boom")
}

// TestConnectModule_PanicRecovery asserts a panicking handler becomes an opaque
// INTERNAL connect error, never leaking the panic value.
func TestConnectModule_PanicRecovery(t *testing.T) {
	t.Parallel()

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, opts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewEchoServiceHandler(panicEcho{}, opts...)
		}),
	)
	testkit.NewRuntimeHarness(t, m)
	addr := testkit.WaitForAddr(t, m).String()

	client := testv1connect.NewEchoServiceClient(h2cClient(), "http://"+addr)
	_, err := client.Echo(context.Background(), connect.NewRequest(&testv1.EchoRequest{Message: "x"}))
	testza.AssertNotNil(t, err)

	var ce *connect.Error
	testza.AssertTrue(t, stderrors.As(err, &ce))
	testza.AssertEqual(t, connect.CodeInternal, ce.Code())
	testza.AssertEqual(t, "internal error", ce.Message())
	testza.AssertNotContains(t, ce.Message(), "handler boom")
}

// greeter is a DI-provided dependency the WithService registrar resolves.
type greeter struct {
	prefix string
}

// TestConnectModule_WithServiceDIResolution asserts a WithService registrar can
// lakta.Invoke a provided dependency during Init (deferred resolution).
func TestConnectModule_WithServiceDIResolution(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	testkit.WithProvider(h, func(_ do.Injector) (*greeter, error) { return &greeter{prefix: "Hi "}, nil })

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(ctx context.Context, opts []connect.HandlerOption) (string, http.Handler) {
			g, err := lakta.Invoke[*greeter](ctx)
			if err != nil {
				return "", nil
			}
			return testv1connect.NewEchoServiceHandler(echoServer{prefix: g.prefix}, opts...)
		}),
	)

	testza.AssertNil(t, m.Init(h.Ctx()))
	go func() { _ = m.Start(context.Background()) }()
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
	addr := testkit.WaitForAddr(t, m).String()

	client := testv1connect.NewEchoServiceClient(h2cClient(), "http://"+addr)
	resp, err := client.Echo(context.Background(), connect.NewRequest(&testv1.EchoRequest{Message: "there"}))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "Hi there", resp.Msg.GetMessage())
}

// TestConnectModule_WithHandlerMountsAsIs asserts a pre-built WithHandler entry
// is mounted verbatim with no interceptor chain injected.
func TestConnectModule_WithHandlerMountsAsIs(t *testing.T) {
	t.Parallel()

	raw := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("raw"))
	})

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithHandler("/raw", raw),
	)
	testkit.NewRuntimeHarness(t, m)
	addr := testkit.WaitForAddr(t, m).String()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/raw", nil)
	testza.AssertNil(t, err)
	resp, err := http.DefaultClient.Do(req)
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	testza.AssertEqual(t, http.StatusTeapot, resp.StatusCode)
}
