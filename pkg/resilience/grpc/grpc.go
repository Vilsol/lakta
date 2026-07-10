// Package grpc adapts named resilience policies into gRPC interceptors.
// Import it aliased (e.g. resgrpc) to avoid clashing with google.golang.org/grpc.
package grpc

import (
	"context"
	"errors"

	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/failsafegrpc"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

// translateOverload rewrites failsafe policy sentinels to gRPC status codes,
// preserving nil and passing non-sentinel errors through untouched:
//
//	bulkhead.ErrFull, adaptivelimiter.ErrExceeded -> codes.Unavailable        (transient; client retries)
//	ratelimiter.ErrExceeded                       -> codes.ResourceExhausted  (quota)
func translateOverload(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bulkhead.ErrFull), errors.Is(err, adaptivelimiter.ErrExceeded):
		return status.Error(codes.Unavailable, "overloaded") //nolint:wrapcheck // status error is the translation, not a wrapped cause
	case errors.Is(err, ratelimiter.ErrExceeded):
		return status.Error(codes.ResourceExhausted, "rate limit exceeded") //nolint:wrapcheck // status error is the translation, not a wrapped cause
	default:
		return err
	}
}

// NewUnaryClientInterceptor runs unary client calls through the named policy.
func NewUnaryClientInterceptor(reg *policy.Registry, name string) (grpc.UnaryClientInterceptor, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}
	return failsafegrpc.NewUnaryClientInterceptorWithExecutor(ex), nil
}

// NewUnaryServerInterceptor runs unary server handlers through the named policy,
// translating overload sentinels to gRPC status codes.
func NewUnaryServerInterceptor(reg *policy.Registry, name string) (grpc.UnaryServerInterceptor, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}
	inner := failsafegrpc.NewUnaryServerInterceptorWithExecutor(ex)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := inner(ctx, req, info, handler)
		return resp, translateOverload(err)
	}, nil
}

// NewServerInHandle guards incoming requests with the named policy before
// message allocation; prefer it over the server interceptor for load limiting.
// It translates overload sentinels to gRPC status codes.
func NewServerInHandle(reg *policy.Registry, name string) (tap.ServerInHandle, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}
	inner := failsafegrpc.NewServerInHandleWithExecutor(ex)
	return func(ctx context.Context, info *tap.Info) (context.Context, error) {
		newCtx, err := inner(ctx, info)
		return newCtx, translateOverload(err)
	}, nil
}
