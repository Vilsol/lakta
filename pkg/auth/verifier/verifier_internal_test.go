package verifier

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// assertOpaqueUnauthenticated asserts err is the single generic 401 detail,
// leaking nothing about which check failed.
func assertOpaqueUnauthenticated(t *testing.T, err error) {
	t.Helper()
	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, unauthenticatedMessage, err.Error())
}

func TestVerify_JWKS_Success(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, testAudience).
			Claim("scope", "read write").
			Claim("realm_access", map[string]any{"roles": []any{"admin", "user"}})
	})

	p, err := reg.Verify(context.Background(), token)
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "user-123", p.Subject)
	testza.AssertEqual(t, testIssuer, p.Issuer)
	testza.AssertTrue(t, p.HasScope("read"))
	testza.AssertTrue(t, p.HasScope("write"))
	testza.AssertTrue(t, p.HasRole("admin"))
	testza.AssertFalse(t, p.HasRole("superuser"))
}

func TestVerify_AlgNone_Rejected(t *testing.T) {
	t.Parallel()
	_, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)
	_, err := reg.Verify(context.Background(), noneToken(t, testIssuer, testAudience))
	assertOpaqueUnauthenticated(t, err)
}

func TestVerify_RS256ToHS256Downgrade_Rejected(t *testing.T) {
	t.Parallel()
	_, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)

	// Classic confusion: HMAC-sign with the issuer's PUBLIC key bytes as secret,
	// keeping the JWKS issuer's iss so it routes to the RS256-only verifier.
	pubBytes := jwksJSON(t, pub)
	token := signHS256(t, pubBytes, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, testAudience)
	})

	_, err := reg.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err)
}

func TestVerify_WrongAudience_Rejected(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer, "api://other-service")
	})

	_, err := reg.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err)
}

func TestVerify_MissingAudience_Rejected(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		now := time.Now()
		return b.Issuer(testIssuer).Subject("user-123").IssuedAt(now).Expiration(now.Add(time.Hour))
	})

	_, err := reg.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err)
}

func TestConfig_EmptyAudience_IsInitError(t *testing.T) {
	t.Parallel()
	_, err := buildIssuers(context.Background(), []IssuerConfig{{
		Issuer:     testIssuer,
		Audience:   nil,
		JWKSURL:    "https://example.com/jwks",
		Algorithms: []string{algRS256},
	}})
	testza.AssertNotNil(t, err)
}

func TestVerify_ExactIssuer_PrefixRejected(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, time.Minute)
	// A look-alike issuer suffix must NOT match the configured issuer.
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		return standardClaims(b, testIssuer+".evil", testAudience)
	})

	_, err := reg.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err)
}

func TestVerify_Expired_Rejected(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	reg := registryForServer(t, srv.URL, []string{algRS256}, 10*time.Second)
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		now := time.Now()
		return b.Issuer(testIssuer).Audience([]string{testAudience}).Subject("user-123").
			IssuedAt(now.Add(-time.Hour)).Expiration(now.Add(-time.Minute))
	})

	_, err := reg.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err)
}

func TestVerify_ClockSkewBoundary(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	// Token expired 30s ago.
	expiredBy30s := func(b *jwt.Builder) *jwt.Builder {
		now := time.Now()
		return b.Issuer(testIssuer).Audience([]string{testAudience}).Subject("user-123").
			IssuedAt(now.Add(-time.Hour)).Expiration(now.Add(-30 * time.Second))
	}
	token := signRS256(t, priv, expiredBy30s)

	inside := registryForServer(t, srv.URL, []string{algRS256}, 60*time.Second)
	_, err := inside.Verify(context.Background(), token)
	testza.AssertNoError(t, err) // 30s < 60s skew -> accepted

	outside := registryForServer(t, srv.URL, []string{algRS256}, 10*time.Second)
	_, err = outside.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err) // 30s > 10s skew -> rejected
}

func TestVerify_ClockSkewCapped(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)

	// Token expired 6 minutes ago; a configured 10m skew is capped to 5m, so it
	// must still be rejected.
	token := signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder {
		now := time.Now()
		return b.Issuer(testIssuer).Audience([]string{testAudience}).Subject("user-123").
			IssuedAt(now.Add(-time.Hour)).Expiration(now.Add(-6 * time.Minute))
	})

	reg := registryForServer(t, srv.URL, []string{algRS256}, 10*time.Minute)
	_, err := reg.Verify(context.Background(), token)
	assertOpaqueUnauthenticated(t, err)
}

func TestVerify_JWKSRotation_NoReload(t *testing.T) {
	t.Parallel()
	priv1, pub1 := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub1))
	srv := jwksServer(t, &body)

	// Build a cache with a short constant refresh so rotation is observable.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	testza.AssertNoError(t, err)
	testza.AssertNoError(t, cache.Register(ctx, srv.URL, jwk.WithConstantInterval(200*time.Millisecond)))
	keyset, err := cache.CachedSet(srv.URL)
	testza.AssertNoError(t, err)

	reg := &Registry{}
	reg.state.Store(&registryState{
		issuers: map[string]*issuerVerifier{testIssuer: {
			issuer:    testIssuer,
			audience:  []string{testAudience},
			algs:      []string{algRS256},
			keyset:    keyset,
			clockSkew: time.Minute,
		}},
		scopeClaim: defaultScopeClaim,
	})

	tok1 := signRS256(t, priv1, func(b *jwt.Builder) *jwt.Builder { return standardClaims(b, testIssuer, testAudience) })
	_, err = reg.Verify(context.Background(), tok1)
	testza.AssertNoError(t, err)

	// Rotate: serve a brand-new key (new kid) and sign with it. No config reload.
	priv2, pub2 := newRSAKeys(t, "kid-2")
	body.Store(jwksJSON(t, pub2))
	tok2 := signRS256(t, priv2, func(b *jwt.Builder) *jwt.Builder { return standardClaims(b, testIssuer, testAudience) })

	deadline := time.Now().Add(5 * time.Second)
	var verr error
	for time.Now().Before(deadline) {
		if _, verr = reg.Verify(context.Background(), tok2); verr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	testza.AssertNoError(t, verr)
}

func TestVerify_OpaqueErrors_IdenticalAcrossCauses(t *testing.T) {
	t.Parallel()
	priv, pub := newRSAKeys(t, "kid-1")
	var body atomic.Value
	body.Store(jwksJSON(t, pub))
	srv := jwksServer(t, &body)
	reg := registryForServer(t, srv.URL, []string{algRS256}, 10*time.Second)

	causes := map[string]string{
		"alg_none":      noneToken(t, testIssuer, testAudience),
		"wrong_aud":     signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder { return standardClaims(b, testIssuer, "api://nope") }),
		"prefix_iss":    signRS256(t, priv, func(b *jwt.Builder) *jwt.Builder { return standardClaims(b, testIssuer+".evil", testAudience) }),
		"garbage_token": "not-a-jwt",
	}

	var seen string
	for name, tok := range causes {
		_, err := reg.Verify(context.Background(), tok)
		testza.AssertNotNil(t, err)
		if seen == "" {
			seen = err.Error()
		}
		testza.AssertEqual(t, seen, err.Error(), "cause %q must render an identical opaque message", name)
	}
	testza.AssertEqual(t, unauthenticatedMessage, seen)
}
