package grpc_test

import (
	"context"
	stderrors "errors"
	"io"
	"testing"

	"github.com/MarvinJWendt/testza"
	pkgerrors "github.com/Vilsol/lakta/pkg/errors"
	errgrpc "github.com/Vilsol/lakta/pkg/errors/grpc"
	valgrpc "github.com/Vilsol/lakta/pkg/validation/grpc"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const testMethod = "/test.Svc/Do"

func newValidator(t *testing.T) *valgrpc.Validator {
	t.Helper()
	v, err := valgrpc.NewValidator()
	testza.AssertNoError(t, err)
	return v
}

// runUnary chains the Stage B interceptor (inner, closest to handler) inside the
// Phase 5 grpc renderer (outer) exactly as the runtime wires them, then returns
// the final wire error.
func runUnary(t *testing.T, req any) error {
	t.Helper()
	inner := valgrpc.UnaryServerInterceptor(newValidator(t))
	outer := errgrpc.UnaryServerInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: testMethod}
	handler := func(_ context.Context, r any) (any, error) { return r, nil }
	_, err := outer(context.Background(), req, info, func(ctx context.Context, r any) (any, error) {
		return inner(ctx, r, info, handler)
	})
	return err
}

func badRequestViolations(t *testing.T, err error) map[string]string {
	t.Helper()
	st, ok := status.FromError(err)
	testza.AssertTrue(t, ok, "expected a grpc status error")
	testza.AssertEqual(t, codes.InvalidArgument, st.Code())

	out := map[string]string{}
	found := false
	for _, d := range st.Details() {
		if br, ok := d.(*errdetails.BadRequest); ok {
			found = true
			for _, fv := range br.GetFieldViolations() {
				out[fv.GetField()] = fv.GetDescription()
			}
		}
	}
	testza.AssertTrue(t, found, "expected errdetails.BadRequest detail")
	return out
}

func TestUnaryInvalidRendersInvalidArgument(t *testing.T) {
	t.Parallel()

	err := runUnary(t, newUser("not-an-email"))
	viols := badRequestViolations(t, err)
	testza.AssertEqual(t, "email", viols["email"])
}

func TestUnaryValidReachesHandler(t *testing.T) {
	t.Parallel()

	reached := false
	inner := valgrpc.UnaryServerInterceptor(newValidator(t))
	info := &grpc.UnaryServerInfo{FullMethod: testMethod}
	handler := func(_ context.Context, r any) (any, error) { reached = true; return r, nil }

	_, err := inner(context.Background(), newUser("a@b.com"), info, handler)
	testza.AssertNoError(t, err)
	testza.AssertTrue(t, reached)
}

func TestUnaryNonProtoRequestSkipped(t *testing.T) {
	t.Parallel()

	inner := valgrpc.UnaryServerInterceptor(newValidator(t))
	info := &grpc.UnaryServerInfo{FullMethod: testMethod}
	handler := func(_ context.Context, r any) (any, error) { return r, nil }

	_, err := inner(context.Background(), "not-a-proto", info, handler)
	testza.AssertNoError(t, err)
}

// recvStream feeds one message then io.EOF, standing in for grpc.ServerStream. It
// sets the email field on whatever message the handler allocated (same descriptor).
type recvStream struct {
	grpc.ServerStream

	email string
	sent  bool
}

func (s *recvStream) Context() context.Context { return context.Background() }

func (s *recvStream) RecvMsg(m any) error {
	if s.sent {
		return io.EOF
	}
	s.sent = true
	pm, ok := m.(protoreflect.ProtoMessage)
	if !ok {
		return stderrors.New("not a proto message")
	}
	pm.ProtoReflect().Set(userMD.Fields().ByName("email"), protoreflect.ValueOfString(s.email))
	return nil
}

func TestStreamInvalidReturnsAppError(t *testing.T) {
	t.Parallel()

	interceptor := valgrpc.StreamServerInterceptor(newValidator(t))
	info := &grpc.StreamServerInfo{FullMethod: "/test.Svc/Stream"}
	stream := &recvStream{email: "not-an-email"}

	handler := func(_ any, ss grpc.ServerStream) error {
		return ss.RecvMsg(newUser(""))
	}

	err := interceptor(nil, stream, info, handler)

	var appErr *pkgerrors.AppError
	testza.AssertTrue(t, stderrors.As(err, &appErr))
	testza.AssertEqual(t, pkgerrors.CodeValidation, appErr.Code)
	testza.AssertEqual(t, "email", appErr.Fields[0].Field)
	testza.AssertEqual(t, "email", appErr.Fields[0].Description)
}

func TestStreamValidReachesHandler(t *testing.T) {
	t.Parallel()

	interceptor := valgrpc.StreamServerInterceptor(newValidator(t))
	info := &grpc.StreamServerInfo{FullMethod: "/test.Svc/Stream"}
	stream := &recvStream{email: "a@b.com"}

	reached := false
	handler := func(_ any, ss grpc.ServerStream) error {
		reached = true
		return ss.RecvMsg(newUser(""))
	}

	err := interceptor(nil, stream, info, handler)
	testza.AssertNoError(t, err)
	testza.AssertTrue(t, reached)
}
