package verifier

import (
	stderrors "errors"

	errpkg "github.com/Vilsol/lakta/pkg/errors"
)

// Reason classifies why a token was rejected. It is for logs and security
// events only; the wire error stays an opaque UNAUTHENTICATED.
type Reason string

// Rejection reasons.
const (
	ReasonMissing         Reason = "missing"
	ReasonMalformed       Reason = "malformed"
	ReasonAlgorithm       Reason = "algorithm"
	ReasonUnknownIssuer   Reason = "unknown_issuer"
	ReasonInvalid         Reason = "invalid"
	ReasonExpired         Reason = "expired"
	ReasonAudience        Reason = "audience"
	ReasonTokenType       Reason = "token_type"
	ReasonKeysUnavailable Reason = "keys_unavailable"
)

type rejection struct {
	reason Reason
	cause  error
}

func (r *rejection) Error() string {
	if r.cause == nil {
		return "token rejected: " + string(r.reason)
	}
	return "token rejected: " + string(r.reason) + ": " + r.cause.Error()
}

func (r *rejection) Unwrap() error { return r.cause }

// ReasonOf returns the rejection reason carried by err, or "".
func ReasonOf(err error) Reason {
	var r *rejection
	if stderrors.As(err, &r) {
		return r.reason
	}
	return ""
}

// Reject builds the opaque UNAUTHENTICATED error carrying reason; transport
// adapters use it for reasons they detect themselves (e.g. a missing token).
func Reject(reason Reason, cause error) error {
	return errpkg.Unauthenticated(unauthenticatedMessage).WithCause(&rejection{reason: reason, cause: cause})
}

func keysUnavailable(cause error) error {
	return errpkg.Unavailable("authentication temporarily unavailable").
		WithCause(&rejection{reason: ReasonKeysUnavailable, cause: cause})
}
