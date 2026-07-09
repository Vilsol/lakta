package errors

import "google.golang.org/grpc/codes"

// Code is the stable, transport-agnostic error classifier. It is the value
// rendered as problem+json `code`, the errdetails.ErrorInfo `Reason`, and the
// `urn:lakta:error:{code}` type URI. Treat the string values as wire contract.
type Code string

// Canonical codes. Each seeds a fixed (HTTP, codes.Code) pair via statusFor;
// constructors read that mapping so a code always renders identically.
//
//	Code                 HTTP  gRPC (codes.Code)
//	NOT_FOUND            404   NotFound
//	INVALID_ARGUMENT     400   InvalidArgument
//	VALIDATION           400   InvalidArgument
//	UNAUTHENTICATED      401   Unauthenticated
//	PERMISSION_DENIED    403   PermissionDenied
//	ALREADY_EXISTS       409   AlreadyExists
//	FAILED_PRECONDITION  400   FailedPrecondition
//	UNAVAILABLE          503   Unavailable
//	INTERNAL             500   Internal
const (
	CodeNotFound           Code = "NOT_FOUND"
	CodeInvalidArgument    Code = "INVALID_ARGUMENT"
	CodeValidation         Code = "VALIDATION"
	CodeUnauthenticated    Code = "UNAUTHENTICATED"
	CodePermissionDenied   Code = "PERMISSION_DENIED"
	CodeAlreadyExists      Code = "ALREADY_EXISTS"
	CodeFailedPrecondition Code = "FAILED_PRECONDITION"
	CodeUnavailable        Code = "UNAVAILABLE"
	CodeInternal           Code = "INTERNAL"
)

const (
	statusBadRequest          = 400
	statusUnauthorized        = 401
	statusForbidden           = 403
	statusNotFound            = 404
	statusConflict            = 409
	statusInternalServerError = 500
	statusServiceUnavailable  = 503
)

// lookup returns the (HTTP, codes.Code) seed for a canonical code. The bool is
// false for an unrecognized code, letting callers distinguish "unknown" from
// the INTERNAL fallback.
func lookup(code Code) (int, codes.Code, bool) {
	switch code {
	case CodeNotFound:
		return statusNotFound, codes.NotFound, true
	case CodeInvalidArgument:
		return statusBadRequest, codes.InvalidArgument, true
	case CodeValidation:
		return statusBadRequest, codes.InvalidArgument, true
	case CodeUnauthenticated:
		return statusUnauthorized, codes.Unauthenticated, true
	case CodePermissionDenied:
		return statusForbidden, codes.PermissionDenied, true
	case CodeAlreadyExists:
		return statusConflict, codes.AlreadyExists, true
	case CodeFailedPrecondition:
		return statusBadRequest, codes.FailedPrecondition, true
	case CodeUnavailable:
		return statusServiceUnavailable, codes.Unavailable, true
	case CodeInternal:
		return statusInternalServerError, codes.Internal, true
	default:
		return 0, codes.OK, false
	}
}

// statusFor returns the (HTTP, codes.Code) seed for code, defaulting to
// (500, codes.Internal) for an unmapped code.
func statusFor(code Code) (int, codes.Code) {
	if httpStatus, grpcCode, ok := lookup(code); ok {
		return httpStatus, grpcCode
	}
	return statusInternalServerError, codes.Internal
}
