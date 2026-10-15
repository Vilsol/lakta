package spiffe_test

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/auth/spiffe"
	"github.com/Vilsol/lakta/pkg/auth/spiffe/spiffetest"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
)

const trustDomain = "errs.test"

func initModule(t *testing.T, socket string) (context.Context, *spiffe.Module, error) {
	t.Helper()
	h := testkit.NewHarness(t)
	lakta.ProvideValue(h.Ctx(), config.Profile("test"))
	m := spiffe.NewModule(spiffe.WithSocketPath(socket), spiffe.WithTrustDomain(trustDomain))
	err := m.Init(h.Ctx())
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
	return h.Ctx(), m, err
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met within 10s")
}

func TestStartsWithAgentDownAndBecomesReady(t *testing.T) {
	t.Parallel()
	api := spiffetest.New(t, trustDomain, "svc", spiffetest.Stopped())

	start := time.Now()
	_, m, err := initModule(t, api.SocketPath())
	testza.AssertNil(t, err)
	testza.AssertTrue(t, time.Since(start) < time.Second, "Init must not wait for the agent")
	testza.AssertNotNil(t, m.Source().Ready(t.Context()))

	api.Start()
	eventually(t, func() bool { return m.Source().Ready(t.Context()) == nil })
}

func TestServerTLS_AcceptsTrustDomainMembersOnly(t *testing.T) {
	t.Parallel()
	api := spiffetest.New(t, trustDomain, "server")
	ctx, m, err := initModule(t, api.SocketPath())
	testza.AssertNil(t, err)
	eventually(t, func() bool { return m.Source().Ready(t.Context()) == nil })

	serverTLS, err := spiffe.ServerTLS()(ctx)
	testza.AssertNil(t, err)
	testza.AssertContains(t, serverTLS.NextProtos, "h2")

	lis, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	testza.AssertNil(t, err)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { _ = srv.Close() })

	get := func(cfg *tls.Config) error {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: true}, Timeout: 5 * time.Second}
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+lis.Addr().String(), nil)
		resp, err := client.Do(req)
		if err != nil {
			return err //nolint:wrapcheck // test helper
		}
		return resp.Body.Close()
	}

	member := api.Issue("caller")
	testza.AssertNil(t, get(member.ClientTLS(api.ID("server"))))

	foreign := spiffetest.New(t, "other.test", "intruder").Issue("intruder")
	testza.AssertNotNil(t, get(foreign.ClientTLS(api.ID("server"))))

	// A client that presents no certificate is refused.
	testza.AssertNotNil(t, get(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})) //nolint:gosec // asserting refusal
}

func TestClientTLS_VerifiesExpectedServerID(t *testing.T) {
	t.Parallel()
	api := spiffetest.New(t, trustDomain, "caller")
	ctx, m, err := initModule(t, api.SocketPath())
	testza.AssertNil(t, err)
	eventually(t, func() bool { return m.Source().Ready(t.Context()) == nil })

	impostor := api.Issue("impostor")
	lis, err := tls.Listen("tcp", "127.0.0.1:0", impostor.ServerTLS())
	testza.AssertNil(t, err)
	go func() {
		for {
			c, aerr := lis.Accept()
			if aerr != nil {
				return
			}
			if tc, ok := c.(*tls.Conn); ok {
				_ = tc.HandshakeContext(context.Background())
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = lis.Close() })

	wantServer, err := spiffe.ClientTLS(api.ID("server").String())(ctx)
	testza.AssertNil(t, err)
	conn, err := (&tls.Dialer{Config: wantServer}).DialContext(t.Context(), "tcp", lis.Addr().String())
	if err == nil {
		if tc, ok := conn.(*tls.Conn); ok {
			err = tc.HandshakeContext(t.Context())
		}
		_ = conn.Close()
	}
	testza.AssertNotNil(t, err, "impostor server must be rejected")
}

func TestRotationWithoutRestart(t *testing.T) {
	t.Parallel()
	api := spiffetest.New(t, trustDomain, "svc")
	_, m, err := initModule(t, api.SocketPath())
	testza.AssertNil(t, err)
	eventually(t, func() bool { return m.Source().Ready(t.Context()) == nil })

	first, err := m.Source().GetX509SVID()
	testza.AssertNil(t, err)
	api.Rotate()
	eventually(t, func() bool {
		svid, gerr := m.Source().GetX509SVID()
		return gerr == nil && svid.Certificates[0].SerialNumber.Cmp(first.Certificates[0].SerialNumber) != 0
	})
}

func TestExpiredSVID_NotReady(t *testing.T) {
	t.Parallel()
	api := spiffetest.New(t, trustDomain, "svc", spiffetest.WithTTL(2*time.Second))
	_, m, err := initModule(t, api.SocketPath())
	testza.AssertNil(t, err)
	eventually(t, func() bool { return m.Source().Ready(t.Context()) == nil })
	eventually(t, func() bool { return m.Source().Ready(t.Context()) != nil })
}

func TestProductionGuard_RefusesLocalTrustDomain(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	lakta.ProvideValue(h.Ctx(), config.Profile("prod"))
	m := spiffe.NewModule(spiffe.WithSocketPath("unix:///nonexistent.sock"), spiffe.WithTrustDomain("errs.local"))
	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

// resumes reports whether a second connection resumed the first one's session.
func resumes(t *testing.T, serverTLS, clientTLS *tls.Config) bool {
	t.Helper()
	lis, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("x"))
			_ = conn.Close()
		}
	}()

	clientTLS = clientTLS.Clone()
	clientTLS.ClientSessionCache = tls.NewLRUClientSessionCache(1)
	resumed := false
	for range 2 {
		conn, err := (&tls.Dialer{Config: clientTLS}).DialContext(t.Context(), "tcp", lis.Addr().String())
		testza.AssertNil(t, err)
		_, _ = io.ReadAll(conn)
		tlsConn, ok := conn.(*tls.Conn)
		testza.AssertTrue(t, ok)
		resumed = tlsConn.ConnectionState().DidResume
		_ = conn.Close()
	}
	return resumed
}

func TestTLS_NoSessionResumption(t *testing.T) {
	t.Parallel()
	api := spiffetest.New(t, trustDomain, "server")
	ctx, m, err := initModule(t, api.SocketPath())
	testza.AssertNil(t, err)
	eventually(t, func() bool { return m.Source().Ready(t.Context()) == nil })

	serverTLS, err := spiffe.ServerTLS()(ctx)
	testza.AssertNil(t, err)
	testza.AssertFalse(t, resumes(t, serverTLS, api.Issue("caller").ClientTLS(api.ID("server"))),
		"a resumed session skips the SVID check against the current trust bundle")

	clientTLS, err := spiffe.ClientTLS(api.ID("peer").String())(ctx)
	testza.AssertNil(t, err)
	testza.AssertFalse(t, resumes(t, api.Issue("peer").ServerTLS(), clientTLS))
}
