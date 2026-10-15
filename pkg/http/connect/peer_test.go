package connect_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	testv1 "github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1"
	"github.com/Vilsol/lakta/pkg/http/connect/internal/gen/test/v1/testv1connect"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	testza.AssertNil(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	testza.AssertNil(t, err)
	cert, err := x509.ParseCertificate(der)
	testza.AssertNil(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

func (ca *testCA) leaf(t *testing.T, spiffeID string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	testza.AssertNil(t, err)
	u, err := url.Parse(spiffeID)
	testza.AssertNil(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), URIs: []*url.URL{u},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	testza.AssertNil(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

const (
	serverID  = "spiffe://errs.test/ns/errs/sa/tmplsvc"
	allowedID = "spiffe://errs.test/ns/errs/sa/gateway"
	otherID   = "spiffe://errs.test/ns/errs/sa/other"
)

// peerServer answers Echo with "<peer>|<client ip>".
type peerServer struct {
	testv1connect.UnimplementedEchoServiceHandler
}

func (peerServer) Echo(ctx context.Context, _ *connect.Request[testv1.EchoRequest]) (*connect.Response[testv1.EchoResponse], error) {
	ip := ""
	if addr, ok := connectmod.ClientIP(ctx); ok {
		ip = addr.String()
	}
	return connect.NewResponse(&testv1.EchoResponse{Message: connectmod.PeerID(ctx) + "|" + ip}), nil
}

func mtlsModules(t *testing.T, ca *testCA, profile config.Profile, buf *syncBuffer, opts ...connectmod.Option) (*connectmod.Module, []lakta.Module) {
	t.Helper()
	serverCert := ca.leaf(t, serverID)
	opts = append([]connectmod.Option{
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		connectmod.WithTLSProvider(func(context.Context) (*tls.Config, error) {
			return &tls.Config{
				Certificates: []tls.Certificate{serverCert},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    ca.pool,
				MinVersion:   tls.VersionTLS12,
				NextProtos:   []string{"h2", "http/1.1"},
			}, nil
		}),
		connectmod.WithService(func(_ context.Context, hopts []connect.HandlerOption) (string, http.Handler) {
			return testv1connect.NewEchoServiceHandler(peerServer{}, hopts...)
		}),
	}, opts...)
	m := connectmod.NewModule(opts...)

	profileModule := testkit.NewMockModule()
	profileModule.OnInit = func(ctx context.Context) error {
		lakta.ProvideValue(ctx, profile)
		return nil
	}

	return m, []lakta.Module{profileModule, loggerModule(buf), m}
}

func startMTLS(t *testing.T, ca *testCA, opts ...connectmod.Option) (string, *syncBuffer) {
	t.Helper()
	buf := &syncBuffer{}
	m, modules := mtlsModules(t, ca, "test", buf, opts...)
	testkit.NewRuntimeHarness(t, modules...)
	return "https://" + testkit.WaitForAddr(t, m).String(), buf
}

func mtlsClient(ca *testCA, cert tls.Certificate) *http.Client {
	return &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{cert}, RootCAs: ca.pool, MinVersion: tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec // the test server cert has no DNS SAN
	}}}
}

func echoAs(t *testing.T, client *http.Client, base string, clientIP string, copts ...connect.ClientOption) (string, error) {
	t.Helper()
	req := connect.NewRequest(&testv1.EchoRequest{})
	if clientIP != "" {
		req.Header().Set("X-Client-IP", clientIP)
	}
	resp, err := testv1connect.NewEchoServiceClient(client, base, copts...).Echo(t.Context(), req)
	if err != nil {
		return "", err //nolint:wrapcheck // tests inspect the raw error
	}
	return resp.Msg.GetMessage(), nil
}

func TestPeer_AllowedCallerReachesHandlerWithPeerID(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	base, _ := startMTLS(t, ca, connectmod.WithAllowedCallers(allowedID))
	got, err := echoAs(t, mtlsClient(ca, ca.leaf(t, allowedID)), base, "")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, allowedID+"|", got)

	// gRPC protocol needs h2 over ALPN.
	got, err = echoAs(t, mtlsClient(ca, ca.leaf(t, allowedID)), base, "", connect.WithGRPC())
	testza.AssertNil(t, err)
	testza.AssertEqual(t, allowedID+"|", got)
}

func TestPeer_UnlistedCallerDeniedAndLogged(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	base, logs := startMTLS(t, ca, connectmod.WithAllowedCallers(allowedID))
	_, err := echoAs(t, mtlsClient(ca, ca.leaf(t, otherID)), base, "")
	testza.AssertEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))
	testza.AssertContains(t, logs.String(), `"log.type":"security_event"`)
	testza.AssertContains(t, logs.String(), otherID)
}

func TestPeer_DenialCarriesRequestID(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	base, logs := startMTLS(t, ca, connectmod.WithAllowedCallers(allowedID))

	const id = "01J9ZK5V7Q8N3XGJ6M2B4C5D6E"
	req := connect.NewRequest(&testv1.EchoRequest{})
	req.Header().Set(connectmod.RequestIDHeader, id)
	_, err := testv1connect.NewEchoServiceClient(mtlsClient(ca, ca.leaf(t, otherID)), base).Echo(t.Context(), req)

	var cerr *connect.Error
	testza.AssertTrue(t, errors.As(err, &cerr))
	testza.AssertEqual(t, id, cerr.Meta().Get(connectmod.RequestIDHeader))
	testza.AssertContains(t, logs.String(), `"request_id":"`+id+`"`)
}

func TestPeer_EmptyAllowListDeniesAll(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	base, _ := startMTLS(t, ca)
	_, err := echoAs(t, mtlsClient(ca, ca.leaf(t, allowedID)), base, "")
	testza.AssertEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestPeer_ClientIPOnlyFromTrustedCallers(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	base, _ := startMTLS(t, ca,
		connectmod.WithAllowedCallers(allowedID, otherID),
		connectmod.WithTrustedIPCallers(allowedID),
	)
	got, err := echoAs(t, mtlsClient(ca, ca.leaf(t, allowedID)), base, "203.0.113.7")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, allowedID+"|203.0.113.7", got)

	got, err = echoAs(t, mtlsClient(ca, ca.leaf(t, otherID)), base, "203.0.113.7")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, otherID+"|", got)

	got, err = echoAs(t, mtlsClient(ca, ca.leaf(t, allowedID)), base, "not-an-ip")
	testza.AssertNil(t, err)
	testza.AssertEqual(t, allowedID+"|", got)
}

func TestPeer_HandshakeFailureIsSecurityEvent(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	base, logs := startMTLS(t, ca, connectmod.WithAllowedCallers(allowedID))

	// No client certificate: the handshake must be refused.
	noCertTLS := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} //nolint:gosec // asserting refusal
	noCert := &http.Client{Transport: &http.Transport{TLSClientConfig: noCertTLS}}
	_, err := echoAs(t, noCert, base, "")
	testza.AssertNotNil(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logs.String(), `"reason":"tls_handshake"`) {
		time.Sleep(20 * time.Millisecond)
	}
	testza.AssertContains(t, logs.String(), `"reason":"tls_handshake"`)
}

func TestPeer_ProductionRequiresMTLSAndCallers(t *testing.T) {
	t.Parallel()
	ca := newCA(t)
	_, modules := mtlsModules(t, ca, "prod", &syncBuffer{}, connectmod.WithRequirePeerAuth(true))
	testza.AssertNotNil(t, lakta.NewRuntime(modules...).RunContext(t.Context()), "empty allowed_callers must fail under production")

	plain := connectmod.NewModule(connectmod.WithHost("127.0.0.1"), connectmod.WithPort(0), connectmod.WithRequirePeerAuth(true))
	profileModule := testkit.NewMockModule()
	profileModule.OnInit = func(ctx context.Context) error {
		lakta.ProvideValue(ctx, config.Profile("prod"))
		return nil
	}
	testza.AssertNotNil(t, lakta.NewRuntime(profileModule, plain).RunContext(t.Context()), "plaintext must fail under production")
}
