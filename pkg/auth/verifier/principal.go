package verifier

import (
	"context"
	"slices"

	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Principal is the verified identity the adapters stash in the request context
// after a successful [Registry.Verify].
type Principal struct {
	Subject  string
	Issuer   string
	Audience []string
	Scopes   []string
	Roles    []string
	Claims   map[string]any
	Token    jwt.Token
}

// principalKeyType is the unexported context key for the stashed Principal.
type principalKeyType struct{}

// PrincipalFrom returns the Principal placed in ctx by the adapters, and false
// when the request is anonymous.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKeyType{}).(*Principal)
	return p, ok
}

// ContextWithPrincipal returns a child ctx carrying p.
func ContextWithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKeyType{}, p)
}

// HasScope reports whether p holds scope s.
func (p *Principal) HasScope(s string) bool {
	return slices.Contains(p.Scopes, s)
}

// HasRole reports whether p holds role r.
func (p *Principal) HasRole(r string) bool {
	return slices.Contains(p.Roles, r)
}
