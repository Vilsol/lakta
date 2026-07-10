package verifier

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	testIssuer   = "https://accounts.example.com"
	testAudience = "api://my-service"
	algRS256     = "RS256"
)

// newRSAKeys returns a matched (private, public) jwk pair tagged with kid and
// RS256 alg (as real IdPs publish).
//
//nolint:ireturn // returns the jwx jwk.Key interface pair, the library's own type
func newRSAKeys(t *testing.T, kid string) (jwk.Key, jwk.Key) {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	testza.AssertNoError(t, err)

	priv, err := jwk.Import(raw)
	testza.AssertNoError(t, err)
	testza.AssertNoError(t, priv.Set(jwk.KeyIDKey, kid))
	testza.AssertNoError(t, priv.Set(jwk.AlgorithmKey, jwa.RS256()))

	pub, err := jwk.PublicKeyOf(priv)
	testza.AssertNoError(t, err)
	testza.AssertNoError(t, pub.Set(jwk.KeyIDKey, kid))
	testza.AssertNoError(t, pub.Set(jwk.AlgorithmKey, jwa.RS256()))
	return priv, pub
}

// jwksJSON marshals a single-key JWK Set as an IdP would serve it.
func jwksJSON(t *testing.T, pub jwk.Key) []byte {
	t.Helper()
	set := jwk.NewSet()
	testza.AssertNoError(t, set.AddKey(pub))
	b, err := json.Marshal(set)
	testza.AssertNoError(t, err)
	return b
}

// jwksServer serves whatever JSON body currently sits in body; rotate it by
// storing new bytes.
func jwksServer(t *testing.T, body *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, max-age=0")
		b, _ := body.Load().([]byte)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// standardClaims applies the common valid claim set (iss/aud/sub/exp/iat).
func standardClaims(b *jwt.Builder, iss, aud string) *jwt.Builder {
	now := time.Now()
	return b.Issuer(iss).
		Audience([]string{aud}).
		Subject("user-123").
		IssuedAt(now).
		Expiration(now.Add(time.Hour))
}

// signRS256 builds and RS256-signs a token with priv (kid header from the key).
func signRS256(t *testing.T, priv jwk.Key, build func(*jwt.Builder) *jwt.Builder) string {
	t.Helper()
	tok, err := build(jwt.NewBuilder()).Build()
	testza.AssertNoError(t, err)
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), priv))
	testza.AssertNoError(t, err)
	return string(signed)
}

// signHS256 builds and HS256-signs a token with the given secret.
func signHS256(t *testing.T, secret []byte, build func(*jwt.Builder) *jwt.Builder) string {
	t.Helper()
	tok, err := build(jwt.NewBuilder()).Build()
	testza.AssertNoError(t, err)
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.HS256(), secret))
	testza.AssertNoError(t, err)
	return string(signed)
}

// noneToken hand-crafts an unsigned alg:none JWT (jwt.Sign refuses to make one).
func noneToken(t *testing.T, iss, aud string) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	header := enc([]byte(`{"alg":"none","typ":"JWT"}`))
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"iss": iss,
		"aud": aud,
		"sub": "user-123",
		"exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(),
	})
	testza.AssertNoError(t, err)
	return header + "." + enc(payload) + "."
}

// registryForServer wires a single JWKS issuer (served at jwksURL) into a
// Registry through the real buildIssuers path.
func registryForServer(t *testing.T, jwksURL string, algs []string, skew time.Duration) *Registry {
	t.Helper()
	issuers, err := buildIssuers(context.Background(), []IssuerConfig{{
		Issuer:     testIssuer,
		Audience:   []string{testAudience},
		JWKSURL:    jwksURL,
		Algorithms: algs,
		ClockSkew:  skew,
	}})
	testza.AssertNoError(t, err)
	r := &Registry{}
	r.state.Store(&registryState{
		issuers:    issuers,
		scopeClaim: defaultScopeClaim,
		rolesClaim: "realm_access.roles",
	})
	return r
}
