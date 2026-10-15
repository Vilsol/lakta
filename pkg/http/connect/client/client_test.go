package connectclient_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/auth/spiffe/spiffetest"
	"github.com/Vilsol/lakta/pkg/auth/verifier"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	connectclient "github.com/Vilsol/lakta/pkg/http/connect/client"
	"github.com/Vilsol/lakta/pkg/testkit"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const procedure = "/test.Svc/Echo"

type downstream struct {
	calls  atomic.Int32
	header atomic.Value // http.Header of the last call
	fail   connect.Code
}

func (d *downstream) handler() http.Handler {
	return connect.NewUnaryHandler(
		procedure,
		func(_ context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
			d.calls.Add(1)
			d.header.Store(req.Header().Clone())
			if d.fail != 0 {
				return nil, connect.NewError(d.fail, errSecret)
			}
			return connect.NewResponse(wrapperspb.String("ok")), nil
		},
	)
}

func (d *downstream) last() http.Header {
	h, _ := d.header.Load().(http.Header)
	return h
}

type env struct {
	api    *spiffetest.WorkloadAPI
	down   *downstream
	server *httptest.Server
}

func newEnv(t *testing.T, serverPath string) *env {
	t.Helper()
	api := spiffetest.New(t, "errs.test", "caller")
	d := &downstream{}
	srv := httptest.NewUnstartedServer(d.handler())
	srv.EnableHTTP2 = true
	srv.TLS = api.Issue(serverPath).ServerTLS()
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return &env{api: api, down: d, server: srv}
}

func (e *env) client(t *testing.T, opts ...connectclient.Option) *connectclient.Client {
	t.Helper()
	caller := e.api.Issue("caller")
	opts = append([]connectclient.Option{
		connectclient.WithBaseURL(e.server.URL),
		connectclient.WithTLSProvider(func(context.Context) (*tls.Config, error) {
			return caller.ClientTLS(e.api.ID("server")), nil
		}),
	}, opts...)
	m := connectclient.NewModule(opts...)
	h := testkit.NewHarness(t)
	testza.AssertNil(t, m.Init(h.Ctx()))
	return m.Client()
}

func call(ctx context.Context, c *connectclient.Client) error {
	_, err := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](c.HTTPClient(), c.BaseURL()+procedure, c.Options()...).
		CallUnary(ctx, connect.NewRequest(wrapperspb.String("hi")))
	return err //nolint:wrapcheck // tests inspect the raw error
}

func userCtx(t *testing.T, raw string) context.Context {
	t.Helper()
	return verifier.ContextWithPrincipal(t.Context(), &verifier.Principal{Subject: "u-1", Raw: verifier.RawToken(raw)})
}

func TestAsUser_ForwardsInboundToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")
	testza.AssertNil(t, call(connectclient.AsUser(userCtx(t, "user-token")), e.client(t)))
	testza.AssertEqual(t, "Bearer user-token", e.down.last().Get("Authorization"))
}

func TestAsUser_WithoutTokenFailsBeforeDialing(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")
	testza.AssertNotNil(t, call(connectclient.AsUser(t.Context()), e.client(t)))
	testza.AssertEqual(t, int32(0), e.down.calls.Load())
}

func TestNoCredentialMode_FailsBeforeDialing(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")
	testza.AssertNotNil(t, call(t.Context(), e.client(t)))
	testza.AssertEqual(t, int32(0), e.down.calls.Load())
}

func TestWithToken_UsesGivenToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")
	testza.AssertNil(t, call(connectclient.WithToken(t.Context(), "smoke"), e.client(t)))
	testza.AssertEqual(t, "Bearer smoke", e.down.last().Get("Authorization"))
}

func TestAsService_UsesClientCredentials(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")

	var gotSecret atomic.Value
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, secret, _ := r.BasicAuth()
		gotSecret.Store(secret)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"svc-token","token_type":"Bearer","expires_in":300}`))
	}))
	t.Cleanup(tokenSrv.Close)

	secretFile := filepath.Join(t.TempDir(), "secret")
	testza.AssertNil(t, os.WriteFile(secretFile, []byte("s3cret\n"), 0o600))

	c := e.client(t, connectclient.WithClientCredentials(tokenSrv.URL, "tmplsvc", secretFile))
	testza.AssertNil(t, call(connectclient.AsService(t.Context()), c))
	testza.AssertEqual(t, "Bearer svc-token", e.down.last().Get("Authorization"))
	testza.AssertEqual(t, "s3cret", gotSecret.Load())
}

func TestPropagatesRequestIDAndVerifiedClientIP(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")

	ctx := connectmod.WithClientIP(userCtx(t, "tok"), netip.MustParseAddr("203.0.113.9"))
	testza.AssertNil(t, call(connectclient.AsUser(ctx), e.client(t)))
	testza.AssertEqual(t, "203.0.113.9", e.down.last().Get("X-Client-IP"))
}

func TestDeadlineCappedAtMax(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")
	testza.AssertNil(t, call(connectclient.WithToken(t.Context(), "t"), e.client(t, connectclient.WithMaxDeadline(2*time.Second))))

	ms, err := strconv.Atoi(e.down.last().Get("Connect-Timeout-Ms"))
	testza.AssertNil(t, err)
	testza.AssertTrue(t, ms > 0 && ms <= 2000, ms)
}

func TestDownstreamErrorNotPassedThrough(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "server")
	e.down.fail = connect.CodeNotFound

	err := call(connectclient.WithToken(t.Context(), "t"), e.client(t))
	app := apperrors.FromError(err)
	testza.AssertEqual(t, apperrors.CodeInternal, app.Code)
	testza.AssertNotContains(t, app.Message, errSecret.Error())
	testza.AssertEqual(t, connect.CodeNotFound, connectclient.DownstreamCode(err))

	e.down.fail = connect.CodeUnavailable
	err = call(connectclient.WithToken(t.Context(), "t"), e.client(t))
	testza.AssertEqual(t, apperrors.CodeUnavailable, apperrors.FromError(err).Code)
}

func TestImpostorServerRejected(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "impostor")
	testza.AssertNotNil(t, call(connectclient.WithToken(t.Context(), "t"), e.client(t)))
	testza.AssertEqual(t, int32(0), e.down.calls.Load())
}
