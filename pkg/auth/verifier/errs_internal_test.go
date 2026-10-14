package verifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

func TestVerify_ReasonsAreReportedInternally(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)
	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Second)

	expired := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, testAudience).Expiration(time.Now().Add(-time.Hour))
	})
	wrongAud := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, "api://other")
	})
	unknownIss := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, "https://evil.example.com", testAudience)
	})

	tests := map[string]struct {
		token string
		want  Reason
	}{
		"malformed":      {"not-a-jwt", ReasonMalformed},
		"expired":        {expired, ReasonExpired},
		"wrong audience": {wrongAud, ReasonAudience},
		"unknown issuer": {unknownIss, ReasonUnknownIssuer},
	}
	for name, tc := range tests {
		_, err := reg.Verify(context.Background(), tc.token)
		testza.AssertNotNil(t, err, name)
		testza.AssertEqual(t, tc.want, ReasonOf(err), name)
		// The wire error stays opaque.
		testza.AssertEqual(t, errpkg.CodeUnauthenticated, errpkg.FromError(err).Code, name)
		testza.AssertEqual(t, unauthenticatedMessage, errpkg.FromError(err).Message, name)
	}
}

func TestVerify_TokenTypeEnforcedWhenConfigured(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := &Registry{}
	issuers, err := buildIssuers(context.Background(), []IssuerConfig{{
		Issuer: testIssuer, Audience: []string{testAudience}, JWKSURL: srv.URL,
		Algorithms: []string{algRS256}, TokenType: "Bearer",
	}})
	testza.AssertNoError(t, err)
	reg.state.Store(&registryState{issuers: issuers})

	refresh := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, testAudience).Claim("typ", "Refresh")
	})
	_, err = reg.Verify(context.Background(), refresh)
	testza.AssertEqual(t, ReasonTokenType, ReasonOf(err))

	bearer := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, testAudience).Claim("typ", "Bearer").Claim("sid", "sess-1")
	})
	p, err := reg.Verify(context.Background(), bearer)
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "sess-1", p.Session)
	testza.AssertEqual(t, bearer, p.Raw.Reveal())
}

func TestRawToken_NeverPrinted(t *testing.T) {
	t.Parallel()
	raw := RawToken("eyJhbGciOi.secret.payload")

	var buf strings.Builder
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", slog.Any("token", raw), slog.Any("p", &Principal{Raw: raw}))
	b, err := json.Marshal(raw)
	testza.AssertNoError(t, err)

	for _, s := range []string{buf.String(), string(b), fmt.Sprint(raw), fmt.Sprintf("%v %+v %#v %s %q", raw, raw, raw, raw, raw)} {
		testza.AssertFalse(t, strings.Contains(s, "secret"), s)
	}
}

func TestLazyJWKS_StartsWithKeycloakDownAndRecovers(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store([]byte("not json")) // endpoint "down"
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder { return standardClaims(b, testIssuer, testAudience) })

	_, err := reg.Verify(context.Background(), token)
	testza.AssertEqual(t, ReasonKeysUnavailable, ReasonOf(err))
	testza.AssertEqual(t, errpkg.CodeUnavailable, errpkg.FromError(err).Code)
	testza.AssertNotNil(t, reg.Ready(context.Background()))

	body.Store(jwksJSON(t, pub))
	_, err = reg.Verify(context.Background(), token)
	testza.AssertNoError(t, err)
	testza.AssertNil(t, reg.Ready(context.Background()))

	// Later refresh failures keep the cached keys.
	body.Store([]byte("not json"))
	_, err = reg.Verify(context.Background(), token)
	testza.AssertNoError(t, err)
	testza.AssertNil(t, reg.Ready(context.Background()))
}

func TestGuards_ProductionProfile(t *testing.T) {
	t.Parallel()

	tests := map[string]Config{
		"no issuers":  {},
		"http issuer": {Issuers: []IssuerConfig{{Issuer: "http://kc/realms/x", Audience: []string{"a"}, Algorithms: []string{algRS256}}}},
		"http jwks url": {
			Issuers: []IssuerConfig{{Issuer: "https://kc/realms/x", JWKSURL: "http://kc/jwks", Audience: []string{"a"}, Algorithms: []string{algRS256}}},
		},
		"static key": {
			Issuers:   []IssuerConfig{{Issuer: "https://kc/realms/x", JWKSURL: "https://kc/jwks", Audience: []string{"a"}, Algorithms: []string{algRS256}}},
			StaticKey: &StaticKey{Secret: "s", Profiles: []string{"prod"}},
		},
	}
	for name, cfg := range tests {
		h := testkit.NewHarness(t)
		lakta.ProvideValue(h.Ctx(), config.Profile(""))
		m := &Module{config: cfg}
		err := m.Init(h.Ctx())
		testza.AssertNotNil(t, err, name)
		_ = m.Shutdown(context.Background())
	}
}

func TestGuards_DevProfileAllowsHTTP(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	lakta.ProvideValue(h.Ctx(), config.Profile("dev"))
	m := &Module{
		config: Config{
			Issuers: []IssuerConfig{
				{Issuer: "http://kc/realms/x", JWKSURL: "http://127.0.0.1:1/jwks", Audience: []string{"a"}, Algorithms: []string{algRS256}},
			},
		},
	}
	testza.AssertNoError(t, m.Init(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
}
