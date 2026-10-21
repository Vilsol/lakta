package authconnect_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/MarvinJWendt/testza"
	authconnect "github.com/Vilsol/lakta/pkg/auth/connect"
	"github.com/Vilsol/lakta/pkg/auth/verifier"
	"github.com/Vilsol/lakta/pkg/config"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	audience   = "errs-api"
	privateRPC = "/test.Svc/Private"
	publicRPC  = "/test.Svc/Public"
	streamRPC  = "/test.Svc/Stream"
)

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

// whoami answers with "<sub>|<sid>|<raw token matches>" or "anonymous".
func whoami(ctx context.Context, _ *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
	p, ok := verifier.PrincipalFrom(ctx)
	if !ok {
		return connect.NewResponse(wrapperspb.String("anonymous")), nil
	}
	raw, _ := authconnect.RawToken(ctx)
	return connect.NewResponse(wrapperspb.String(p.Subject + "|" + authconnect.Session(ctx) + "|" + raw.Reveal())), nil
}

func streamDeadline(ctx context.Context, _ *connect.Request[wrapperspb.StringValue], s *connect.ServerStream[wrapperspb.StringValue]) error {
	deadline, ok := ctx.Deadline()
	msg := "none"
	if ok {
		msg = time.Until(deadline).Round(time.Second).String()
	}
	return s.Send(wrapperspb.String(msg)) //nolint:wrapcheck // test double
}

type env struct {
	base string
	jwks *testkit.JWKS
	logs *syncBuffer
}

func start(t *testing.T, jwksUp bool) *env {
	t.Helper()
	return startWith(t, jwksUp, connectmod.WithSlot(connectmod.SlotAuth, authconnect.New(
		authconnect.WithPublicProcedures(func(p string) bool { return p == publicRPC }),
	)))
}

// startWith serves the test procedures with authOpts wiring authentication.
func startWith(t *testing.T, jwksUp bool, authOpts ...connectmod.Option) *env {
	t.Helper()

	jwks := testkit.NewJWKS(t)
	jwks.SetAvailable(jwksUp)
	logs := &syncBuffer{}

	logger := testkit.NewMockModule()
	logger.OnInit = func(ctx context.Context) error {
		lakta.ProvideValue(ctx, slogJSON(logs))
		return nil
	}

	server := connectmod.NewModule(append([]connectmod.Option{
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithService(func(_ context.Context, opts []connect.HandlerOption) (string, http.Handler) {
			mux := http.NewServeMux()
			mux.Handle(privateRPC, connect.NewUnaryHandler(privateRPC, whoami, opts...))
			mux.Handle(publicRPC, connect.NewUnaryHandler(publicRPC, whoami, opts...))
			mux.Handle(streamRPC, connect.NewServerStreamHandler(streamRPC, streamDeadline, opts...))
			return "/test.Svc/", mux
		}),
	}, authOpts...)...)

	testkit.NewRuntimeHarness(t,
		config.NewModule(config.WithConfigDirs("/nonexistent"), config.WithProfile("test")),
		logger,
		verifier.NewModule(verifier.WithIssuers(verifier.IssuerConfig{
			Issuer:     jwks.IssuerURL("qms-internal"),
			Audience:   []string{audience},
			Algorithms: []string{"RS256"},
			TokenType:  "Bearer",
		})),
		server,
	)

	return &env{base: "http://" + testkit.WaitForAddr(t, server).String(), jwks: jwks, logs: logs}
}

func (e *env) token(opts ...testkit.TokenOption) string {
	claims := map[string]any{"iss": e.jwks.IssuerURL("qms-internal"), "aud": audience, "sub": "user-1", "sid": "sess-1", "typ": "Bearer"}
	return e.jwks.Sign(claims, opts...)
}

func call(t *testing.T, base, procedure, token string) (string, error) {
	t.Helper()
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](http.DefaultClient, base+procedure)
	req := connect.NewRequest(wrapperspb.String(""))
	if token != "" {
		req.Header().Set("Authorization", "Bearer "+token)
	}
	resp, err := client.CallUnary(t.Context(), req)
	if err != nil {
		return "", err //nolint:wrapcheck // tests inspect the raw error
	}
	return resp.Msg.GetValue(), nil
}

func TestMissingToken_RejectedAndLogged(t *testing.T) {
	t.Parallel()
	e := start(t, true)

	_, err := call(t, e.base, privateRPC, "")
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	testza.AssertContains(t, e.logs.String(), `"log.type":"security_event"`)
	testza.AssertContains(t, e.logs.String(), `"reason":"missing"`)
}

func TestPublicProcedure_AllowsMissingButNotInvalidToken(t *testing.T) {
	t.Parallel()
	e := start(t, true)

	got, err := call(t, e.base, publicRPC, "")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "anonymous", got)

	_, err = call(t, e.base, publicRPC, "garbage.token.value")
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestValidToken_PrincipalRawTokenAndSession(t *testing.T) {
	t.Parallel()
	e := start(t, true)
	tok := e.token()

	got, err := call(t, e.base, privateRPC, tok)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "user-1|sess-1|"+tok, got)
	testza.AssertFalse(t, strings.Contains(e.logs.String(), tok), "token must never be logged")
}

func TestExpiredToken_ReasonLogged(t *testing.T) {
	t.Parallel()
	e := start(t, true)

	_, err := call(t, e.base, privateRPC, e.token(testkit.WithExpiresIn(-time.Hour)))
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	testza.AssertContains(t, e.logs.String(), `"reason":"expired"`)
}

func TestKeysUnavailable_IsUnavailable(t *testing.T) {
	t.Parallel()
	e := start(t, false)

	_, err := call(t, e.base, privateRPC, e.token())
	testza.AssertEqual(t, connect.CodeUnavailable, connect.CodeOf(err))
}

func TestStream_RequiresTokenAndEndsAtExpiry(t *testing.T) {
	t.Parallel()
	e := start(t, true)
	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](http.DefaultClient, e.base+streamRPC)

	stream, err := client.CallServerStream(t.Context(), connect.NewRequest(wrapperspb.String("")))
	testza.AssertNil(t, err)
	for stream.Receive() {
		t.Fatal("unauthenticated stream must not deliver messages")
	}
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(stream.Err()))

	req := connect.NewRequest(wrapperspb.String(""))
	req.Header().Set("Authorization", "Bearer "+e.token(testkit.WithExpiresIn(30*time.Second)))
	stream, err = client.CallServerStream(t.Context(), req)
	testza.AssertNil(t, err)
	testza.AssertTrue(t, stream.Receive())
	// exp has second granularity, so the remaining time is 29-30s.
	testza.AssertTrue(t, stream.Msg().GetValue() == "29s" || stream.Msg().GetValue() == "30s", stream.Msg().GetValue())
}
