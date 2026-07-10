package verifier

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/samber/oops"
)

// unauthenticatedMessage is the single, generic detail returned for EVERY
// verification failure so distinct causes are indistinguishable to the client.
const unauthenticatedMessage = "authentication required"

// unauthenticated builds the opaque 401 every failure path returns.
func unauthenticated() error {
	return errpkg.Unauthenticated(unauthenticatedMessage)
}

// registryState is the immutable verifier set swapped atomically on hot-reload.
type registryState struct {
	issuers    map[string]*issuerVerifier
	staticKey  *staticVerifier
	scopeClaim string
	rolesClaim string
}

// Registry holds one verifier per configured issuer plus the isolated static-key
// verifier. It is the single entry point both adapters call.
type Registry struct {
	state atomic.Pointer[registryState]
}

// issuerVerifier binds one issuer to its JWKS-backed keyset, alg allowlist and
// audience.
type issuerVerifier struct {
	issuer    string
	audience  []string
	algs      []string
	keyset    jwk.Set
	clockSkew time.Duration
}

// staticVerifier is the HS256-only dev path; it can never reach a JWKS keyset.
type staticVerifier struct {
	secret    []byte
	audience  []string
	clockSkew time.Duration
}

// Verify validates rawToken end-to-end and returns the Principal. Every failure
// path returns an opaque errors.Unauthenticated that never reveals which check
// failed. See the phase design for the numbered sequence.
func (r *Registry) Verify(_ context.Context, rawToken string) (*Principal, error) {
	st := r.state.Load()
	if st == nil {
		return nil, unauthenticated()
	}

	raw := []byte(rawToken)

	// 1. Read the JWS header algorithm WITHOUT verifying the signature.
	alg, ok := headerAlgorithm(raw)
	if !ok {
		return nil, unauthenticated()
	}
	// alg:none (and any empty alg) is rejected outright, before key selection.
	if alg == "" || alg == jwa.NoSignature().String() {
		return nil, unauthenticated()
	}

	// 1b. Read the unverified issuer purely to route to a keyset.
	unverified, err := jwt.Parse(raw, jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		return nil, unauthenticated()
	}
	iss, _ := unverified.Issuer()

	// 2. Exact-match iss to a configured issuer; else the isolated static path.
	if iv, found := st.issuers[iss]; found {
		// 3. Enforce the alg allowlist BEFORE key selection.
		if !slices.Contains(iv.algs, alg) {
			return nil, unauthenticated()
		}
		tok, verr := iv.verify(raw)
		if verr != nil {
			return nil, unauthenticated()
		}
		return buildPrincipal(tok, st.scopeClaim, st.rolesClaim), nil
	}

	// The static path is reachable only for HS256 tokens whose iss matches no
	// configured issuer, and only when a static key is configured+permitted.
	if st.staticKey != nil && alg == staticAlgorithm {
		tok, verr := st.staticKey.verify(raw)
		if verr != nil {
			return nil, unauthenticated()
		}
		return buildPrincipal(tok, st.scopeClaim, st.rolesClaim), nil
	}

	return nil, unauthenticated()
}

// headerAlgorithm returns the JWS protected-header `alg` without verifying the
// signature.
func headerAlgorithm(raw []byte) (string, bool) {
	msg, err := jws.Parse(raw)
	if err != nil {
		return "", false
	}
	sigs := msg.Signatures()
	if len(sigs) == 0 {
		return "", false
	}
	alg, ok := sigs[0].ProtectedHeaders().Algorithm()
	if !ok {
		return "", false
	}
	return alg.String(), true
}

// verify checks the signature against the issuer's JWKS keyset and validates
// iss (exact), exp/nbf (within the capped skew) and audience intersection.
//
//nolint:ireturn // returns the jwx jwt.Token interface, the library's own type
func (iv *issuerVerifier) verify(raw []byte) (jwt.Token, error) {
	tok, err := jwt.Parse(raw,
		jwt.WithKeySet(iv.keyset, jws.WithInferAlgorithmFromKey(false)),
		jwt.WithValidate(true),
		jwt.WithIssuer(iv.issuer),
		jwt.WithAcceptableSkew(iv.clockSkew),
	)
	if err != nil {
		return nil, oops.Wrapf(err, "jwt parse")
	}
	aud, _ := tok.Audience()
	if !intersects(aud, iv.audience) {
		return nil, oops.Errorf("audience mismatch")
	}
	return tok, nil
}

// verify checks the HS256 signature against the static secret and validates
// exp/nbf; it never touches a JWKS keyset.
//
//nolint:ireturn // returns the jwx jwt.Token interface, the library's own type
func (sv *staticVerifier) verify(raw []byte) (jwt.Token, error) {
	tok, err := jwt.Parse(raw,
		jwt.WithKey(jwa.HS256(), sv.secret),
		jwt.WithValidate(true),
		jwt.WithAcceptableSkew(sv.clockSkew),
	)
	if err != nil {
		return nil, oops.Wrapf(err, "jwt parse")
	}
	if len(sv.audience) > 0 {
		aud, _ := tok.Audience()
		if !intersects(aud, sv.audience) {
			return nil, oops.Errorf("audience mismatch")
		}
	}
	return tok, nil
}

// intersects reports whether have and want share at least one element.
func intersects(have, want []string) bool {
	for _, w := range want {
		if slices.Contains(have, w) {
			return true
		}
	}
	return false
}

// buildPrincipal materializes the verified token into a Principal, extracting
// scopes and roles from the configured claim paths.
func buildPrincipal(tok jwt.Token, scopeClaim, rolesClaim string) *Principal {
	sub, _ := tok.Subject()
	iss, _ := tok.Issuer()
	aud, _ := tok.Audience()

	claims := make(map[string]any, len(tok.Keys()))
	for _, k := range tok.Keys() {
		var v any
		if err := tok.Get(k, &v); err == nil {
			claims[k] = v
		}
	}

	return &Principal{
		Subject:  sub,
		Issuer:   iss,
		Audience: aud,
		Scopes:   scopeValues(claimByPath(claims, scopeClaim)),
		Roles:    roleValues(claimByPath(claims, rolesClaim)),
		Claims:   claims,
		Token:    tok,
	}
}

// claimByPath walks a dotted path (e.g. "realm_access.roles") through nested
// claim maps and returns the leaf value, or nil when any segment is missing.
func claimByPath(claims map[string]any, path string) any {
	if path == "" {
		return nil
	}
	segments := strings.Split(path, ".")
	var current any = claims
	for _, seg := range segments {
		m, ok := asStringMap(current)
		if !ok {
			return nil
		}
		current, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return current
}

// asStringMap normalizes the map shapes jwx may decode nested claims into.
func asStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// scopeValues renders an OAuth scope claim: a space-delimited string or a list.
func scopeValues(v any) []string {
	switch t := v.(type) {
	case string:
		return strings.Fields(t)
	case []string:
		return t
	case []any:
		return anySliceToStrings(t)
	default:
		return nil
	}
}

// roleValues renders a roles claim: a single string or a list.
func roleValues(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []string:
		return t
	case []any:
		return anySliceToStrings(t)
	default:
		return nil
	}
}

// anySliceToStrings keeps only the string elements of a decoded JSON array.
func anySliceToStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
