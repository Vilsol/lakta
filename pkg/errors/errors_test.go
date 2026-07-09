package errors_test

import (
	stderrors "errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
)

func TestConstructorsSeedStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  *errpkg.AppError
		code errpkg.Code
		http int
		grpc codes.Code
	}{
		{"NotFound", errpkg.NotFound("m"), errpkg.CodeNotFound, 404, codes.NotFound},
		{"InvalidArgument", errpkg.InvalidArgument("m"), errpkg.CodeInvalidArgument, 400, codes.InvalidArgument},
		{"Validation", errpkg.Validation("m"), errpkg.CodeValidation, 400, codes.InvalidArgument},
		{"Unauthenticated", errpkg.Unauthenticated("m"), errpkg.CodeUnauthenticated, 401, codes.Unauthenticated},
		{"PermissionDenied", errpkg.PermissionDenied("m"), errpkg.CodePermissionDenied, 403, codes.PermissionDenied},
		{"AlreadyExists", errpkg.AlreadyExists("m"), errpkg.CodeAlreadyExists, 409, codes.AlreadyExists},
		{"FailedPrecondition", errpkg.FailedPrecondition("m"), errpkg.CodeFailedPrecondition, 400, codes.FailedPrecondition},
		{"Unavailable", errpkg.Unavailable("m"), errpkg.CodeUnavailable, 503, codes.Unavailable},
		{"Internal", errpkg.Internal("m"), errpkg.CodeInternal, 500, codes.Internal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testza.AssertEqual(t, tc.code, tc.err.Code)
			testza.AssertEqual(t, tc.http, tc.err.HTTP)
			testza.AssertEqual(t, tc.grpc, tc.err.GRPC)
			testza.AssertEqual(t, "m", tc.err.Message)
			testza.AssertEqual(t, "m", tc.err.Error())
		})
	}
}

func TestNewUnknownCodeDefaultsInternal(t *testing.T) {
	t.Parallel()

	err := errpkg.New("SOMETHING_ELSE", "boom")
	testza.AssertEqual(t, 500, err.HTTP)
	testza.AssertEqual(t, codes.Internal, err.GRPC)
}

func TestBuildersChainWithoutMutatingInputs(t *testing.T) {
	t.Parallel()

	cause := stderrors.New("root cause")
	base := errpkg.InvalidArgument("bad")

	enriched := base.WithField("user.email", "required").WithMeta("k", "v").WithCause(cause)

	testza.AssertEqual(t, 1, len(enriched.Fields))
	testza.AssertEqual(t, "user.email", enriched.Fields[0].Field)
	testza.AssertEqual(t, "required", enriched.Fields[0].Description)
	testza.AssertEqual(t, "v", enriched.Meta["k"])
	testza.AssertErrorIs(t, enriched, cause)
	testza.AssertEqual(t, cause, enriched.Unwrap())

	testza.AssertEqual(t, "root cause", cause.Error())
}

func TestFromErrorNil(t *testing.T) {
	t.Parallel()

	testza.AssertNil(t, errpkg.FromError(nil))
}

func TestFromErrorAppErrorPassthrough(t *testing.T) {
	t.Parallel()

	orig := errpkg.NotFound("gone").WithMeta("k", "v")
	got := errpkg.FromError(orig)

	testza.AssertEqual(t, orig, got)
	testza.AssertEqual(t, errpkg.CodeNotFound, got.Code)
}

func TestFromErrorAppErrorWrapped(t *testing.T) {
	t.Parallel()

	orig := errpkg.NotFound("gone")
	wrapped := oops.Wrapf(orig, "context")
	got := errpkg.FromError(wrapped)

	testza.AssertEqual(t, errpkg.CodeNotFound, got.Code)
}

func TestFromErrorOopsLift(t *testing.T) {
	t.Parallel()

	oopsErr := oops.Code("VALIDATION").With("field", "email").Errorf("invalid")
	got := errpkg.FromError(oopsErr)

	testza.AssertEqual(t, errpkg.CodeValidation, got.Code)
	testza.AssertEqual(t, "email", got.Meta["field"])
	testza.AssertErrorIs(t, got, oopsErr)
}

func TestFromErrorOopsUnknownCode(t *testing.T) {
	t.Parallel()

	oopsErr := oops.Code("weird_domain_code").Errorf("nope")
	got := errpkg.FromError(oopsErr)

	testza.AssertEqual(t, errpkg.CodeInternal, got.Code)
	testza.AssertErrorIs(t, got, oopsErr)
}

func TestFromErrorPlainInternal(t *testing.T) {
	t.Parallel()

	plain := stderrors.New("boom")
	got := errpkg.FromError(plain)

	testza.AssertEqual(t, errpkg.CodeInternal, got.Code)
	testza.AssertEqual(t, "internal error", got.Message)
	testza.AssertEqual(t, 500, got.HTTP)
	testza.AssertEqual(t, codes.Internal, got.GRPC)
	testza.AssertErrorIs(t, got, plain)
	testza.AssertEqual(t, plain, got.Unwrap())
}
