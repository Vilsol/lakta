package connect_test

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/MarvinJWendt/testza"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	testv1 "github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1"
	"github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1/testv1connect"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/Vilsol/slox"
	"github.com/samber/oops"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // test sink
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// behaviorServer switches on the request message so one service exercises
// every slot.
type behaviorServer struct{}

func (behaviorServer) Echo(ctx context.Context, req *connect.Request[testv1.EchoRequest]) (*connect.Response[testv1.EchoResponse], error) {
	switch req.Msg.GetMessage() {
	case "panic":
		panic("secret-panic-value")
	case "notfound":
		return nil, oops.In("note").Code("NOTE_NOT_FOUND").Tags(apperrors.ClassNotFound).
			With("email", "jane@example.com").Public("note not found").Errorf("lookup jane@example.com")
	case "injector":
		return connect.NewResponse(&testv1.EchoResponse{Message: strconv.FormatBool(lakta.HasInjector(ctx))}), nil
	case "reqid":
		return connect.NewResponse(&testv1.EchoResponse{Message: connectmod.RequestID(ctx)}), nil
	case "deadline":
		deadline, ok := ctx.Deadline()
		if !ok {
			return connect.NewResponse(&testv1.EchoResponse{Message: "none"}), nil
		}
		return connect.NewResponse(&testv1.EchoResponse{Message: time.Until(deadline).Round(time.Second).String()}), nil
	case "log":
		slox.Info(ctx, "inside handler")
		return connect.NewResponse(&testv1.EchoResponse{}), nil
	}
	return connect.NewResponse(&testv1.EchoResponse{Message: req.Msg.GetMessage()}), nil
}

func (behaviorServer) EchoStream(_ context.Context, req *connect.Request[testv1.EchoRequest], stream *connect.ServerStream[testv1.EchoResponse]) error {
	return stream.Send(&testv1.EchoResponse{Message: req.Msg.GetMessage()}) //nolint:wrapcheck // test double
}

// loggerModule provides a JSON logger writing into buf so tests can assert log lines.
func loggerModule(buf *syncBuffer) *testkit.MockModule {
	m := testkit.NewMockModule()
	m.OnInit = func(ctx context.Context) error {
		lakta.ProvideValue(ctx, slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		return nil
	}
	return m
}

func startBehavior(t *testing.T, opts ...connectmod.Option) (string, *syncBuffer) {
	t.Helper()

	buf := &syncBuffer{}
	opts = append([]connectmod.Option{
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, hopts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewEchoServiceHandler(behaviorServer{}, hopts...)
		}),
	}, opts...)
	m := connectmod.NewModule(opts...)
	testkit.NewRuntimeHarness(t, loggerModule(buf), m)

	return "http://" + testkit.WaitForAddr(t, m).String(), buf
}

func echo(t *testing.T, base, msg string, copts ...connect.ClientOption) (*connect.Response[testv1.EchoResponse], error) {
	t.Helper()
	client := testv1connect.NewEchoServiceClient(h2cClient(), base, copts...)
	return client.Echo(t.Context(), connect.NewRequest(&testv1.EchoRequest{Message: msg})) //nolint:wrapcheck // tests inspect the raw error
}

func errorInfo(t *testing.T, err error) *errdetails.ErrorInfo {
	t.Helper()
	var cerr *connect.Error
	testza.AssertTrue(t, stderrors.As(err, &cerr))
	for _, d := range cerr.Details() {
		v, derr := d.Value()
		if derr != nil {
			continue
		}
		if info, ok := v.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	t.Fatal("no ErrorInfo detail")
	return nil
}

func TestBoundary_OopsErrorMappedWithoutLeak(t *testing.T) {
	t.Parallel()

	base, logs := startBehavior(t)
	_, err := echo(t, base, "notfound")

	testza.AssertEqual(t, connect.CodeNotFound, connect.CodeOf(err))
	info := errorInfo(t, err)
	testza.AssertEqual(t, "NOTE_NOT_FOUND", info.GetReason())
	testza.AssertEqual(t, "note", info.GetDomain())
	testza.AssertEqual(t, 0, len(info.GetMetadata()))
	testza.AssertNotContains(t, err.Error(), "jane@example.com")
	testza.AssertContains(t, err.Error(), "note not found")

	testza.AssertContains(t, logs.String(), `"level":"WARN"`)
	testza.AssertContains(t, logs.String(), "jane@example.com")
}

func TestBoundary_PanicIsOpaqueInternalAndLoggedAsError(t *testing.T) {
	t.Parallel()

	base, logs := startBehavior(t)
	_, err := echo(t, base, "panic")

	testza.AssertEqual(t, connect.CodeInternal, connect.CodeOf(err))
	testza.AssertNotContains(t, err.Error(), "secret-panic-value")
	testza.AssertContains(t, logs.String(), `"level":"ERROR"`)
	testza.AssertContains(t, logs.String(), "secret-panic-value")
}

// rejectAll is a full interceptor (unary + streaming) standing in for an auth slot.
type rejectAll struct{}

func (rejectAll) WrapUnary(connect.UnaryFunc) connect.UnaryFunc {
	return func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, oops.Code("TOKEN_MISSING").Tags(apperrors.ClassUnauthenticated).Public("unauthenticated").Errorf("no token")
	}
}

func (rejectAll) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (rejectAll) WrapStreamingHandler(connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(context.Context, connect.StreamingHandlerConn) error {
		return oops.Code("TOKEN_MISSING").Tags(apperrors.ClassUnauthenticated).Public("unauthenticated").Errorf("no token")
	}
}

func TestSlots_AuthSlotErrorPassesBoundaryUnaryAndStream(t *testing.T) {
	t.Parallel()

	base, _ := startBehavior(t, connectmod.WithSlot(connectmod.SlotAuth, func(context.Context) (connect.Interceptor, error) {
		return rejectAll{}, nil
	}))

	_, err := echo(t, base, "hi")
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	testza.AssertEqual(t, "TOKEN_MISSING", errorInfo(t, err).GetReason())

	client := testv1connect.NewEchoServiceClient(h2cClient(), base)
	stream, err := client.EchoStream(t.Context(), connect.NewRequest(&testv1.EchoRequest{Message: "hi"}))
	testza.AssertNil(t, err)
	for stream.Receive() {
		t.Fatal("stream must not deliver messages when auth rejects")
	}
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(stream.Err()))
}

func TestSlots_FactoryErrorFailsInit(t *testing.T) {
	t.Parallel()

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithSlot(connectmod.SlotAuthz, func(context.Context) (connect.Interceptor, error) {
			return nil, stderrors.New("no rules")
		}),
	)
	testza.AssertNotNil(t, lakta.NewRuntime(m).RunContext(t.Context()))
}

func TestHandlerCtx_CarriesInjectorAndLogger(t *testing.T) {
	t.Parallel()

	base, logs := startBehavior(t)

	resp, err := echo(t, base, "injector")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "true", resp.Msg.GetMessage())

	_, err = echo(t, base, "log")
	testza.AssertNil(t, err)
	testza.AssertContains(t, logs.String(), "inside handler")
}

func TestRequestID_AcceptsValidGeneratesOtherwise(t *testing.T) {
	t.Parallel()

	base, logs := startBehavior(t)
	const valid = "0192b7a4-7c1e-7d0a-9a5e-3f1c2b4d5e6f"

	withHeader := func(v string) connect.ClientOption {
		return connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				req.Header().Set("X-Request-Id", v)
				return next(ctx, req)
			}
		}))
	}

	resp, err := echo(t, base, "reqid", withHeader(valid))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, valid, resp.Msg.GetMessage())
	testza.AssertEqual(t, valid, resp.Header().Get("X-Request-Id"))

	resp, err = echo(t, base, "reqid", withHeader("../../etc/passwd"))
	testza.AssertNil(t, err)
	generated := resp.Msg.GetMessage()
	testza.AssertNotEqual(t, "", generated)
	testza.AssertNotEqual(t, "../../etc/passwd", generated)
	testza.AssertEqual(t, generated, resp.Header().Get("X-Request-Id"))

	_, err = echo(t, base, "log", withHeader(valid))
	testza.AssertNil(t, err)
	testza.AssertContains(t, logs.String(), `"request_id":"`+valid+`"`)
}

func TestDeadline_DefaultAppliedAndClientDeadlineCapped(t *testing.T) {
	t.Parallel()

	base, _ := startBehavior(t, connectmod.WithDefaultDeadline(3*time.Second), connectmod.WithMaxDeadline(5*time.Second))

	resp, err := echo(t, base, "deadline")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "3s", resp.Msg.GetMessage())

	resp, err = echo(t, base, "deadline", connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			ctx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			return next(ctx, req)
		}
	})))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "5s", resp.Msg.GetMessage())
}

func TestLimits_OversizedUnaryRejected(t *testing.T) {
	t.Parallel()

	base, _ := startBehavior(t, connectmod.WithReadMaxBytes(1024))

	_, err := echo(t, base, strings.Repeat("x", 64*1024))
	testza.AssertEqual(t, connect.CodeResourceExhausted, connect.CodeOf(err))

	resp, err := echo(t, base, "small")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "small", resp.Msg.GetMessage())
}

func TestLimits_GetDisabledByDefault(t *testing.T) {
	t.Parallel()

	base, _ := startBehavior(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+testv1connect.EchoServiceEchoProcedure+"?encoding=json&message={}", nil)
	testza.AssertNil(t, err)
	resp, err := http.DefaultClient.Do(req)
	testza.AssertNil(t, err)
	_ = resp.Body.Close()
	testza.AssertEqual(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestMountedHook_ReceivesServiceDescriptors(t *testing.T) {
	t.Parallel()

	var (
		mu  sync.Mutex
		got []connectmod.MountedService
	)
	startBehavior(t, connectmod.WithMountedHook(func(_ context.Context, services []connectmod.MountedService) error {
		mu.Lock()
		defer mu.Unlock()
		got = services
		return nil
	}))

	mu.Lock()
	defer mu.Unlock()
	testza.AssertEqual(t, 1, len(got))
	testza.AssertEqual(t, "/test.v1.EchoService/", got[0].Path)
	testza.AssertEqual(t, "test.v1.EchoService", string(got[0].Descriptor.FullName()))
	testza.AssertEqual(t, 2, got[0].Descriptor.Methods().Len())
}

func TestMountedHook_ErrorFailsInit(t *testing.T) {
	t.Parallel()

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, hopts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewEchoServiceHandler(behaviorServer{}, hopts...)
		}),
		connectmod.WithMountedHook(func(context.Context, []connectmod.MountedService) error {
			return stderrors.New("rpc without rule")
		}),
	)
	testza.AssertNotNil(t, lakta.NewRuntime(m).RunContext(t.Context()))
}

func TestBoundary_RecordsErrorOnSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	base, _ := startBehavior(t, connectmod.WithSlot(connectmod.SlotOtel, func(context.Context) (connect.Interceptor, error) {
		return otelconnect.NewInterceptor(otelconnect.WithTracerProvider(tp))
	}))

	_, _ = echo(t, base, "notfound")
	_, _ = echo(t, base, "panic")

	spans := recorder.Ended()
	testza.AssertEqual(t, 2, len(spans))

	notFound, panicked := spans[0], spans[1]
	var exceptions []sdktrace.Event
	for _, e := range notFound.Events() {
		if e.Name == "exception" {
			exceptions = append(exceptions, e)
		}
	}
	testza.AssertEqual(t, 1, len(exceptions))
	testza.AssertContains(t, fmt.Sprint(exceptions[0].Attributes), "NOTE_NOT_FOUND")
	testza.AssertNotEqual(t, otelcodes.Error, notFound.Status().Code)
	testza.AssertEqual(t, otelcodes.Error, panicked.Status().Code)
}
