package testkit_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
)

func fetch(t *testing.T, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	testza.AssertNil(t, err)
	resp, err := http.DefaultClient.Do(req)
	testza.AssertNil(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	testza.AssertNil(t, err)
	return resp.StatusCode, b
}

func TestJWKS_DiscoveryAndKeys(t *testing.T) {
	t.Parallel()
	j := testkit.NewJWKS(t)

	code, body := fetch(t, j.IssuerURL("qms-internal")+"/.well-known/openid-configuration")
	testza.AssertEqual(t, http.StatusOK, code)
	var doc map[string]string
	testza.AssertNil(t, json.Unmarshal(body, &doc))
	testza.AssertEqual(t, j.IssuerURL("qms-internal"), doc["issuer"])
	testza.AssertEqual(t, j.JWKSURL(), doc["jwks_uri"])

	code, body = fetch(t, j.JWKSURL())
	testza.AssertEqual(t, http.StatusOK, code)
	testza.AssertContains(t, string(body), `"kty":"RSA"`)

	j.SetAvailable(false)
	code, _ = fetch(t, j.JWKSURL())
	testza.AssertEqual(t, http.StatusServiceUnavailable, code)
}

func TestJWKS_SignAddsTimesAndHeader(t *testing.T) {
	t.Parallel()
	j := testkit.NewJWKS(t)

	tok := j.Sign(map[string]any{"iss": j.IssuerURL("r"), "sub": "u"}, testkit.WithExpiresIn(-time.Minute))
	parts := strings.Split(tok, ".")
	testza.AssertEqual(t, 3, len(parts))

	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	testza.AssertNil(t, err)
	testza.AssertContains(t, string(header), `"alg":"RS256"`)

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	testza.AssertNil(t, err)
	var claims map[string]any
	testza.AssertNil(t, json.Unmarshal(payload, &claims))
	exp, ok := claims["exp"].(float64)
	testza.AssertTrue(t, ok)
	testza.AssertTrue(t, exp < float64(time.Now().Unix()))
	testza.AssertNotNil(t, claims["iat"])
}
