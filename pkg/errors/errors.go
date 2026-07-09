// Package errors is the transport-agnostic error substrate. An AppError value
// carries a canonical Code plus the seeded HTTP and gRPC statuses, and renders
// to both problem+json (via pkg/errors/fiber) and status+errdetails (via
// pkg/errors/grpc). The core imports only google.golang.org/grpc/codes as a
// transport-adjacent leaf; framework-error mapping lives in the renderers.
package errors

import (
	stderrors "errors"

	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
)

// FieldViolation names one invalid input field and why it failed. Populated
// only by validation (Phase 6); auth (Phase 11) must never set it.
type FieldViolation struct {
	Field       string // dotted/bracketed path, e.g. "user.email" or "items[0].qty"
	Description string // failing rule, e.g. "required", "email", "min"
}

// AppError is the transport-agnostic error substrate. One value renders to both
// problem+json (fiber) and status+errdetails (gRPC). Construct via the code
// constructors; enrich via the With* builders. The zero value is not valid.
type AppError struct {
	Code    Code              // canonical classifier
	Message string            // human-safe, client-visible message
	HTTP    int               // seeded from Code; the fiber renderer's status
	GRPC    codes.Code        // seeded from Code; the gRPC renderer's status code
	Meta    map[string]string // rendered as errdetails.ErrorInfo.Metadata
	Fields  []FieldViolation  // rendered as invalid_params / errdetails.BadRequest
	cause   error             // wrapped origin; never serialized to the wire
}

// Error returns Message only (never the cause / never Meta) so an accidental
// %v never leaks internals into a log line that reaches a client.
func (e *AppError) Error() string {
	return e.Message
}

// Unwrap exposes the wrapped cause for errors.Is/As chaining.
func (e *AppError) Unwrap() error {
	return e.cause
}

// WithField appends a FieldViolation and returns e for chaining.
func (e *AppError) WithField(field, desc string) *AppError {
	e.Fields = append(e.Fields, FieldViolation{Field: field, Description: desc})
	return e
}

// WithMeta sets Meta[k]=v (lazily allocating Meta) and returns e for chaining.
func (e *AppError) WithMeta(k, v string) *AppError {
	if e.Meta == nil {
		e.Meta = make(map[string]string)
	}
	e.Meta[k] = v
	return e
}

// WithCause attaches the wrapped origin error (surfaced via Unwrap; kept off the
// wire) and returns e for chaining.
func (e *AppError) WithCause(err error) *AppError {
	e.cause = err
	return e
}

// New builds an AppError for code with the given message, seeding HTTP and GRPC
// from the mapping table.
func New(code Code, msg string) *AppError {
	httpStatus, grpcCode := statusFor(code)
	return &AppError{
		Code:    code,
		Message: msg,
		HTTP:    httpStatus,
		GRPC:    grpcCode,
	}
}

// NotFound builds a NOT_FOUND AppError.
func NotFound(msg string) *AppError { return New(CodeNotFound, msg) }

// InvalidArgument builds an INVALID_ARGUMENT AppError.
func InvalidArgument(msg string) *AppError { return New(CodeInvalidArgument, msg) }

// Validation builds a VALIDATION AppError (field-enumerating; Phase 6 producer).
func Validation(msg string) *AppError { return New(CodeValidation, msg) }

// Unauthenticated builds an UNAUTHENTICATED AppError.
func Unauthenticated(msg string) *AppError { return New(CodeUnauthenticated, msg) }

// PermissionDenied builds a PERMISSION_DENIED AppError.
func PermissionDenied(msg string) *AppError { return New(CodePermissionDenied, msg) }

// AlreadyExists builds an ALREADY_EXISTS AppError.
func AlreadyExists(msg string) *AppError { return New(CodeAlreadyExists, msg) }

// FailedPrecondition builds a FAILED_PRECONDITION AppError.
func FailedPrecondition(msg string) *AppError { return New(CodeFailedPrecondition, msg) }

// Unavailable builds an UNAVAILABLE AppError.
func Unavailable(msg string) *AppError { return New(CodeUnavailable, msg) }

// Internal builds an INTERNAL AppError.
func Internal(msg string) *AppError { return New(CodeInternal, msg) }

// FromError normalizes any error into a non-nil *AppError (nil in -> nil out).
// Precedence:
//  1. errors.As(*AppError): return it unchanged (already normalized).
//  2. oops error (has Code()/Context()): map its Code() to a canonical Code when
//     it matches, else CodeInternal; lift Context() into Meta (stringified);
//     preserve the oops error as cause.
//  3. otherwise: Internal("internal error") with err kept as cause.
//
// Framework errors (fiber.Error, grpc status) are NOT mapped here — that lives
// in the transport renderers so the core stays free of fiber/genproto imports.
func FromError(err error) *AppError {
	if err == nil {
		return nil
	}

	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr
	}

	if oopsErr, ok := oops.AsOops(err); ok {
		code := CodeInternal
		if c := stringifyCode(oopsErr.Code()); c != "" {
			if _, _, ok := lookup(Code(c)); ok {
				code = Code(c)
			}
		}

		result := New(code, err.Error()).WithCause(err)
		for k, v := range oopsErr.Context() {
			result = result.WithMeta(k, stringifyValue(v))
		}
		return result
	}

	return Internal("internal error").WithCause(err)
}
