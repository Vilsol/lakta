// Package grpc adapts the auth verifier into gRPC server interceptors and
// in-handler guards. Import it aliased (e.g. authgrpc) to avoid clashing with
// google.golang.org/grpc. Every failure returns a Phase 5 AppError that the
// Phase 5 grpc renderer turns into an opaque Unauthenticated/PermissionDenied
// status.
package grpc

import (
	"context"

	"github.com/Vilsol/lakta/pkg/auth/verifier"
	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"github.com/samber/oops"
	"google.golang.org/grpc"
)

const (
	// bearerScheme is the Authorization metadata scheme AuthFromMD strips.
	bearerScheme = "bearer"
	// unauthenticatedMessage matches the verifier's opaque 401 detail so every
	// authentication failure renders an identical status message.
	unauthenticatedMessage = "authentication required"
	// forbiddenMessage is the opaque 403 detail for every authorization failure.
	forbiddenMessage = "permission denied"
)

// NewUnaryServerInterceptor builds a unary interceptor whose AuthFunc extracts
// the bearer token, verifies it via reg and stashes the *Principal in the
// context (or returns an opaque Unauthenticated). Wrap it in
// selector.UnaryServerInterceptor to exclude anonymous methods. The instance
// argument is accepted for symmetry; reg already binds one instance.
func NewUnaryServerInterceptor(reg *verifier.Registry, _ string) (grpc.UnaryServerInterceptor, error) {
	if reg == nil {
		return nil, oops.Errorf("auth: registry must not be nil")
	}
	return auth.UnaryServerInterceptor(authFunc(reg)), nil
}

// NewStreamServerInterceptor is the streaming equivalent.
func NewStreamServerInterceptor(reg *verifier.Registry, _ string) (grpc.StreamServerInterceptor, error) {
	if reg == nil {
		return nil, oops.Errorf("auth: registry must not be nil")
	}
	return auth.StreamServerInterceptor(authFunc(reg)), nil
}

// RequireScope is an in-handler guard: PermissionDenied when the ctx Principal
// lacks scope, Unauthenticated when there is no Principal.
func RequireScope(ctx context.Context, scope string) error {
	p, ok := verifier.PrincipalFrom(ctx)
	if !ok {
		return errpkg.Unauthenticated(unauthenticatedMessage)
	}
	if !p.HasScope(scope) {
		return errpkg.PermissionDenied(forbiddenMessage)
	}
	return nil
}

// RequireRole is the role equivalent of RequireScope.
func RequireRole(ctx context.Context, role string) error {
	p, ok := verifier.PrincipalFrom(ctx)
	if !ok {
		return errpkg.Unauthenticated(unauthenticatedMessage)
	}
	if !p.HasRole(role) {
		return errpkg.PermissionDenied(forbiddenMessage)
	}
	return nil
}

// authFunc adapts reg.Verify to go-grpc-middleware's auth.AuthFunc, reading the
// token via auth.AuthFromMD(ctx, "bearer").
func authFunc(reg *verifier.Registry) auth.AuthFunc {
	return func(ctx context.Context) (context.Context, error) {
		token, err := auth.AuthFromMD(ctx, bearerScheme)
		if err != nil {
			return ctx, errpkg.Unauthenticated(unauthenticatedMessage)
		}
		p, verr := reg.Verify(ctx, token)
		if verr != nil {
			return ctx, verr //nolint:wrapcheck // verifier returns a renderable AppError; must pass through unwrapped
		}
		return verifier.ContextWithPrincipal(ctx, p), nil
	}
}

// mTLS Subject-from-SAN interceptor: a future addition that derives
// Principal.Subject from the verified client certificate's SAN. Not built this
// phase -- documented only.
