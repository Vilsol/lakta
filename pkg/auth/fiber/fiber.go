// Package fiber adapts the auth verifier into fiber v3 guard middleware. Import
// it aliased (e.g. authfiber) to avoid clashing with gofiber/fiber. Every
// failure returns a Phase 5 AppError that the fiber ErrorHandler renders as an
// opaque RFC 9457 problem+json 401/403.
package fiber

import (
	"slices"
	"strings"

	"github.com/Vilsol/lakta/pkg/auth/verifier"
	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/gofiber/fiber/v3"
	"github.com/samber/oops"
)

const (
	authorizationHeader = "Authorization"
	bearerPrefix        = "Bearer "
	// unauthenticatedMessage matches the verifier's opaque 401 detail so every
	// authentication failure renders an identical body.
	unauthenticatedMessage = "authentication required"
	// forbiddenMessage is the opaque 403 detail for every authorization failure.
	forbiddenMessage = "permission denied"
)

// New extracts the bearer token, verifies it via reg, stashes the *Principal in
// the request context and calls Next. On any failure it returns an opaque 401.
// The instance argument is accepted for call-site symmetry; reg already binds a
// single verifier instance. Returns an error only if reg is nil.
func New(reg *verifier.Registry, _ string) (fiber.Handler, error) {
	if reg == nil {
		return nil, oops.Errorf("auth: registry must not be nil")
	}
	return func(c fiber.Ctx) error {
		token, ok := bearerToken(c)
		if !ok {
			return errpkg.Unauthenticated(unauthenticatedMessage)
		}
		p, err := reg.Verify(c.Context(), token)
		if err != nil {
			return err //nolint:wrapcheck // verifier returns a renderable AppError; must pass through unwrapped
		}
		c.SetContext(verifier.ContextWithPrincipal(c.Context(), p))
		return c.Next()
	}, nil
}

// Optional behaves like New but allows anonymous requests: a missing or invalid
// token stashes no Principal and still calls Next; a valid token stashes it.
func Optional(reg *verifier.Registry, _ string) (fiber.Handler, error) {
	if reg == nil {
		return nil, oops.Errorf("auth: registry must not be nil")
	}
	return func(c fiber.Ctx) error {
		if token, ok := bearerToken(c); ok {
			if p, err := reg.Verify(c.Context(), token); err == nil {
				c.SetContext(verifier.ContextWithPrincipal(c.Context(), p))
			}
		}
		return c.Next()
	}, nil
}

// RequireScope guards a route: 403 when the ctx Principal lacks scope, 401 when
// there is no Principal. Compose after New in the handler chain.
func RequireScope(scope string) fiber.Handler {
	return requirePrincipal(func(p *verifier.Principal) bool { return p.HasScope(scope) })
}

// RequireRole is the role equivalent of RequireScope.
func RequireRole(role string) fiber.Handler {
	return requirePrincipal(func(p *verifier.Principal) bool { return p.HasRole(role) })
}

// RequireAny passes when the Principal holds ANY of the scopes; 403 otherwise.
func RequireAny(scopes ...string) fiber.Handler {
	return requirePrincipal(func(p *verifier.Principal) bool {
		return slices.ContainsFunc(scopes, p.HasScope)
	})
}

// RequireAll passes only when the Principal holds ALL of the scopes.
func RequireAll(scopes ...string) fiber.Handler {
	return requirePrincipal(func(p *verifier.Principal) bool {
		for _, s := range scopes {
			if !p.HasScope(s) {
				return false
			}
		}
		return true
	})
}

// requirePrincipal builds a guard that 401s without a Principal and 403s when
// check fails.
func requirePrincipal(check func(*verifier.Principal) bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := verifier.PrincipalFrom(c.Context())
		if !ok {
			return errpkg.Unauthenticated(unauthenticatedMessage)
		}
		if !check(p) {
			return errpkg.PermissionDenied(forbiddenMessage)
		}
		return c.Next()
	}
}

// bearerToken pulls the token from an "Authorization: Bearer <token>" header.
func bearerToken(c fiber.Ctx) (string, bool) {
	header := c.Get(authorizationHeader)
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	return header[len(bearerPrefix):], true
}
