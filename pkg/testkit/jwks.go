package testkit

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	jwksKeyID      = "testkit-1"
	defaultTokenTT = 5 * time.Minute
	rsaKeyBits     = 2048
)

// JWKS is an in-process OIDC issuer for tests: per-realm discovery documents
// under /realms/<realm>, one RS256 key served at /jwks, and a signer. It uses
// only the standard library.
type JWKS struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	available atomic.Bool
}

// NewJWKS starts the issuer; it stops when the test ends.
func NewJWKS(t *testing.T) *JWKS {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, rsaKeyBits)
	if err != nil {
		t.Fatalf("testkit: generate rsa key: %v", err)
	}

	j := &JWKS{key: key}
	j.available.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /jwks", j.serveKeys)
	mux.HandleFunc("GET /realms/{realm}/.well-known/openid-configuration", j.serveDiscovery)

	j.srv = httptest.NewServer(mux)
	t.Cleanup(j.srv.Close)

	return j
}

// URL is the server base URL.
func (j *JWKS) URL() string { return j.srv.URL }

// IssuerURL is the issuer of a realm, Keycloak style.
func (j *JWKS) IssuerURL(realm string) string { return j.srv.URL + "/realms/" + realm }

// JWKSURL is where the key set is served.
func (j *JWKS) JWKSURL() string { return j.srv.URL + "/jwks" }

// SetAvailable toggles every endpoint between serving and 503, to simulate the
// identity provider being down.
func (j *JWKS) SetAvailable(up bool) { j.available.Store(up) }

// TokenOption adjusts a token built by Sign.
type TokenOption func(claims map[string]any)

// WithExpiresIn sets exp relative to now; negative values produce expired tokens.
func WithExpiresIn(d time.Duration) TokenOption {
	return func(claims map[string]any) { claims["exp"] = time.Now().Add(d).Unix() }
}

// Sign returns an RS256 JWT over claims. iat and exp (now+5m) are added when absent.
func (j *JWKS) Sign(claims map[string]any, opts ...TokenOption) string {
	all := make(map[string]any, len(claims)+2) //nolint:mnd // iat + exp
	maps.Copy(all, claims)
	if _, ok := all["iat"]; !ok {
		all["iat"] = time.Now().Unix()
	}
	if _, ok := all["exp"]; !ok {
		all["exp"] = time.Now().Add(defaultTokenTT).Unix()
	}
	for _, o := range opts {
		o(all)
	}

	header := mustJSON(map[string]string{"alg": "RS256", "typ": "JWT", "kid": jwksKeyID})
	signingInput := b64(header) + "." + b64(mustJSON(all))

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, j.key, crypto.SHA256, digest[:])
	if err != nil {
		panic("testkit: sign token: " + err.Error())
	}

	return signingInput + "." + b64(sig)
}

func (j *JWKS) serveKeys(w http.ResponseWriter, _ *http.Request) {
	if !j.available.Load() {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	pub := j.key.PublicKey
	writeJSON(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": jwksKeyID,
		"alg": "RS256",
		"use": "sig",
		"n":   b64(pub.N.Bytes()),
		"e":   b64(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (j *JWKS) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	if !j.available.Load() {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]string{
		"issuer":   j.IssuerURL(r.PathValue("realm")),
		"jwks_uri": j.JWKSURL(),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(mustJSON(v))
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("testkit: marshal: " + err.Error())
	}
	return b
}

func b64(b []byte) string { return strings.TrimRight(base64.RawURLEncoding.EncodeToString(b), "=") }
