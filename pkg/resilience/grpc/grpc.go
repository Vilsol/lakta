// Package grpc adapts named resilience policies into gRPC interceptors.
// Import it aliased (e.g. resgrpc) to avoid clashing with google.golang.org/grpc.
package grpc

import (
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/failsafe-go/failsafe-go/failsafegrpc"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/tap"
)

// NewUnaryClientInterceptor runs unary client calls through the named policy.
func NewUnaryClientInterceptor(reg *policy.Registry, name string) (grpc.UnaryClientInterceptor, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}
	return failsafegrpc.NewUnaryClientInterceptorWithExecutor(ex), nil
}

// NewUnaryServerInterceptor runs unary server handlers through the named policy.
func NewUnaryServerInterceptor(reg *policy.Registry, name string) (grpc.UnaryServerInterceptor, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}
	return failsafegrpc.NewUnaryServerInterceptorWithExecutor(ex), nil
}

// NewServerInHandle guards incoming requests with the named policy before
// message allocation; prefer it over the server interceptor for load limiting.
func NewServerInHandle(reg *policy.Registry, name string) (tap.ServerInHandle, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}
	return failsafegrpc.NewServerInHandleWithExecutor(ex), nil
}
