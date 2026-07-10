package verifier

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const staticSecret = "dev-only-super-secret-value" //nolint:gosec // test-only fixture secret

// registryWithStatic wires one JWKS issuer plus an isolated HS256 static path.
func registryWithStatic(t *testing.T, jwksURL, secret string) *Registry {
	t.Helper()
	issuers, err := buildIssuers(context.Background(), []IssuerConfig{{
		Issuer:     testIssuer,
		Audience:   []string{testAudience},
		JWKSURL:    jwksURL,
		Algorithms: []string{algRS256},
	}})
	testza.AssertNoError(t, err)
	r := &Registry{}
	r.state.Store(&registryState{
		issuers:    issuers,
		staticKey:  &staticVerifier{secret: []byte(secret), clockSkew: time.Minute},
		scopeClaim: defaultScopeClaim,
	})
	return r
}

func TestStatic_VerifiesOwnTokens(t *testing.T) {
	t.Parallel()
	_, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryWithStatic(t, srv.URL, staticSecret)

	// HS256 token, iss that does NOT match the JWKS issuer -> static path.
	tok := signHS256(t, []byte(staticSecret), func(b *jwt.Builder) *jwt.Builder {
		now := time.Now()
		return b.Issuer("dev-cli").Subject("dev").IssuedAt(now).Expiration(now.Add(time.Hour))
	})
	p, err := reg.Verify(context.Background(), tok)
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "dev", p.Subject)
}

func TestStatic_RejectsForeignSecret(t *testing.T) {
	t.Parallel()
	_, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)
	reg := registryWithStatic(t, srv.URL, staticSecret)

	tok := signHS256(t, []byte("a-totally-different-secret"), func(b *jwt.Builder) *jwt.Builder {
		now := time.Now()
		return b.Issuer("dev-cli").Subject("dev").IssuedAt(now).Expiration(now.Add(time.Hour))
	})
	_, err := reg.Verify(context.Background(), tok)
	assertOpaqueUnauthenticated(t, err)
}

func TestStatic_CannotForgeJWKSIssuerToken(t *testing.T) {
	t.Parallel()
	_, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)
	reg := registryWithStatic(t, srv.URL, staticSecret)

	// HS256 signed with the static secret but claiming the JWKS issuer's iss:
	// it routes to the RS256-only JWKS verifier and is rejected -- the static
	// path is unreachable for a configured issuer.
	tok := signHS256(t, []byte(staticSecret), func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, testAudience)
	})
	_, err := reg.Verify(context.Background(), tok)
	assertOpaqueUnauthenticated(t, err)
}

func TestInit_StaticProfileGate_PermittedProfile(t *testing.T) {
	t.Setenv(profileEnvVar, "dev")
	h := testkit.NewHarness(t)

	m := NewModule(WithName("default"))
	m.config.StaticKey = &StaticKey{Secret: staticSecret, Profiles: []string{"dev", "local", "test"}}

	err := m.Init(h.Ctx())
	testza.AssertNoError(t, err)
	testza.AssertNotNil(t, m.registry.state.Load().staticKey)
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
}

func TestInit_StaticProfileGate_UnsetProfileRefused(t *testing.T) {
	t.Setenv(profileEnvVar, "")
	h := testkit.NewHarness(t)

	m := NewModule(WithName("default"))
	m.config.StaticKey = &StaticKey{Secret: staticSecret}

	err := m.Init(h.Ctx())
	testza.AssertNotNil(t, err)
}

func TestInit_StaticProfileGate_OutsideAllowlistRefused(t *testing.T) {
	t.Setenv(profileEnvVar, "prod")
	h := testkit.NewHarness(t)

	m := NewModule(WithName("default"))
	m.config.StaticKey = &StaticKey{Secret: staticSecret} // defaults to [dev,local,test]

	err := m.Init(h.Ctx())
	testza.AssertNotNil(t, err)
}

func TestInit_NoStaticKey_Succeeds(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithName("default"))
	err := m.Init(h.Ctx())
	testza.AssertNoError(t, err)
	testza.AssertNil(t, m.registry.state.Load().staticKey)
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
}
