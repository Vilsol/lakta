// Package authconnect verifies bearer JWTs on Connect calls. New returns a
// factory for the connect module's SlotAuth:
//
//	connect.WithSlot(connect.SlotAuth, authconnect.New(authconnect.WithPublicProcedures(isPublic)))
//
// or, with an authorizer that fills the authz slot too, WithAuthorizer.
//
// A missing token passes only for public procedures; an invalid token is
// always rejected. Rejections are opaque UNAUTHENTICATED errors and emit a
// security event with the internal reason. Streams are cancelled when the
// token expires.
package authconnect

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/Vilsol/lakta/pkg/auth/verifier"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/security"
	"github.com/samber/oops"
)

type options struct {
	public   func(procedure string) bool
	registry *verifier.Registry
}

// Option configures the interceptor.
type Option func(*options)

// WithPublicProcedures marks procedures that accept calls without a token.
func WithPublicProcedures(fn func(procedure string) bool) Option {
	return func(o *options) { o.public = fn }
}

// WithRegistry uses r instead of the *verifier.Registry from DI.
func WithRegistry(r *verifier.Registry) Option {
	return func(o *options) { o.registry = r }
}

// New returns a connect slot factory; it resolves *verifier.Registry from DI
// during the connect module's Init unless WithRegistry is given.
func New(opts ...Option) func(ctx context.Context) (connect.Interceptor, error) {
	o := options{public: func(string) bool { return false }}
	for _, opt := range opts {
		opt(&o)
	}

	return func(ctx context.Context) (connect.Interceptor, error) {
		reg := o.registry
		if reg == nil {
			var err error
			reg, err = lakta.Invoke[*verifier.Registry](ctx)
			if err != nil {
				return nil, oops.Wrapf(err, "authconnect: no *verifier.Registry in DI")
			}
		}
		return &interceptor{registry: reg, public: o.public}, nil
	}
}

// RawToken returns the verified bearer token of the current call.
func RawToken(ctx context.Context) (verifier.RawToken, bool) {
	p, ok := verifier.PrincipalFrom(ctx)
	if !ok {
		return "", false
	}
	return p.Raw, true
}

// Session returns the verified token's sid claim, or "".
func Session(ctx context.Context) string {
	p, ok := verifier.PrincipalFrom(ctx)
	if !ok {
		return ""
	}
	return p.Session
}

type interceptor struct {
	registry *verifier.Registry
	public   func(string) bool
}

func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, _, err := i.authenticate(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, p, err := i.authenticate(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		if p != nil && p.Token != nil {
			if exp, ok := p.Token.Expiration(); ok {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, exp)
				defer cancel()
			}
		}
		return next(ctx, conn)
	}
}

func (i *interceptor) authenticate(ctx context.Context, procedure string, header http.Header) (context.Context, *verifier.Principal, error) {
	token, ok := bearer(header.Get("Authorization"))
	if !ok {
		if i.public(procedure) {
			return ctx, nil, nil
		}
		security.Emit(ctx, security.Event{Reason: string(verifier.ReasonMissing), Procedure: procedure})
		return ctx, nil, verifier.Reject(verifier.ReasonMissing, nil) //nolint:wrapcheck // the error boundary renders it
	}

	p, err := i.registry.Verify(ctx, token)
	if err != nil {
		if reason := verifier.ReasonOf(err); reason != verifier.ReasonKeysUnavailable {
			security.Emit(ctx, security.Event{Reason: string(reason), Procedure: procedure})
		}
		return ctx, nil, err //nolint:wrapcheck // opaque AppError for the error boundary
	}

	return verifier.ContextWithPrincipal(ctx, p), p, nil
}

func bearer(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}
