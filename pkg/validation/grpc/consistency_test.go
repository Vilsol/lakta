package grpc_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	pkgerrors "github.com/Vilsol/lakta/pkg/errors"
	valfiber "github.com/Vilsol/lakta/pkg/validation/fiber"
	valgrpc "github.com/Vilsol/lakta/pkg/validation/grpc"
	"google.golang.org/grpc"
)

type emailDTO struct {
	Email string `json:"email" validate:"email"`
}

func asAppError(t *testing.T, err error) *pkgerrors.AppError {
	t.Helper()
	var appErr *pkgerrors.AppError
	testza.AssertTrue(t, stderrors.As(err, &appErr), "expected *AppError")
	return appErr
}

// TestCrossTransportConsistency is the point of the phase: the same logical
// failure (an invalid email) routed through validator/v10 (fiber) and
// protovalidate (grpc) must produce the byte-identical AppError the two Phase 5
// renderers serialize — same Code and same []FieldViolation.
func TestCrossTransportConsistency(t *testing.T) {
	t.Parallel()

	fiberErr := valfiber.New().Validate(&emailDTO{Email: "not-an-email"})
	fiberApp := asAppError(t, fiberErr)

	inner := valgrpc.UnaryServerInterceptor(newValidator(t))
	info := &grpc.UnaryServerInfo{FullMethod: testMethod}
	handler := func(_ context.Context, r any) (any, error) { return r, nil }
	_, grpcErr := inner(context.Background(), newUser("not-an-email"), info, handler)
	grpcApp := asAppError(t, grpcErr)

	testza.AssertEqual(t, pkgerrors.CodeValidation, fiberApp.Code)
	testza.AssertEqual(t, fiberApp.Code, grpcApp.Code)
	testza.AssertEqual(t, []pkgerrors.FieldViolation{{Field: "email", Description: "email"}}, fiberApp.Fields)
	testza.AssertEqual(t, fiberApp.Fields, grpcApp.Fields)
}
