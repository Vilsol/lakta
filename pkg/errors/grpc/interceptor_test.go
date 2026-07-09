package grpc_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	errpkg "github.com/Vilsol/lakta/pkg/errors"
	errgrpc "github.com/Vilsol/lakta/pkg/errors/grpc"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func invokeUnary(t *testing.T, handlerErr error) *status.Status {
	t.Helper()
	interceptor := errgrpc.UnaryServerInterceptor()
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{},
		func(_ context.Context, _ any) (any, error) { return nil, handlerErr })
	st, ok := status.FromError(err)
	testza.AssertTrue(t, ok)
	return st
}

func findErrorInfo(st *status.Status) *errdetails.ErrorInfo {
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

func findBadRequest(st *status.Status) *errdetails.BadRequest {
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			return br
		}
	}
	return nil
}

func TestUnaryInterceptorPassesThroughNil(t *testing.T) {
	t.Parallel()
	interceptor := errgrpc.UnaryServerInterceptor()
	resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{},
		func(_ context.Context, _ any) (any, error) { return "ok", nil })
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "ok", resp)
}

func TestUnaryInterceptorRendersAppError(t *testing.T) {
	t.Parallel()
	st := invokeUnary(t, errpkg.NotFound("gone").WithMeta("resource", "user"))

	testza.AssertEqual(t, codes.NotFound, st.Code())
	testza.AssertEqual(t, "gone", st.Message())

	info := findErrorInfo(st)
	testza.AssertNotNil(t, info)
	testza.AssertEqual(t, "NOT_FOUND", info.GetReason())
	testza.AssertEqual(t, "user", info.GetMetadata()["resource"])

	testza.AssertNil(t, findBadRequest(st))
}

func TestUnaryInterceptorRendersBadRequest(t *testing.T) {
	t.Parallel()
	st := invokeUnary(t, errpkg.Validation("invalid").
		WithField("user.email", "required").
		WithField("user.age", "min"))

	testza.AssertEqual(t, codes.InvalidArgument, st.Code())

	br := findBadRequest(st)
	testza.AssertNotNil(t, br)
	testza.AssertEqual(t, 2, len(br.GetFieldViolations()))
	testza.AssertEqual(t, "user.email", br.GetFieldViolations()[0].GetField())
	testza.AssertEqual(t, "required", br.GetFieldViolations()[0].GetDescription())
}

func TestUnaryInterceptorPlainErrorIsInternal(t *testing.T) {
	t.Parallel()
	st := invokeUnary(t, stderrors.New("boom secret"))

	testza.AssertEqual(t, codes.Internal, st.Code())
	testza.AssertEqual(t, "internal error", st.Message())
	testza.AssertNotContains(t, st.Message(), "boom secret")

	info := findErrorInfo(st)
	testza.AssertNotNil(t, info)
	testza.AssertEqual(t, "INTERNAL", info.GetReason())
}

func TestStreamInterceptorRendersAppError(t *testing.T) {
	t.Parallel()
	interceptor := errgrpc.StreamServerInterceptor()
	err := interceptor(nil, nil, &grpc.StreamServerInfo{},
		func(_ any, _ grpc.ServerStream) error { return errpkg.PermissionDenied("nope") })
	st, ok := status.FromError(err)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, codes.PermissionDenied, st.Code())

	info := findErrorInfo(st)
	testza.AssertNotNil(t, info)
	testza.AssertEqual(t, "PERMISSION_DENIED", info.GetReason())
}
