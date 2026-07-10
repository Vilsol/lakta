// Package grpc runs buf.build/go/protovalidate over request messages and fails
// as a lakta *errors.AppError{Code: VALIDATION}. Import it aliased (e.g. valgrpc)
// to avoid clashing with google.golang.org/grpc.
//
// This is a thin, own interceptor (NOT go-grpc-middleware's stock protovalidate
// one): a CEL violation becomes an AppError carrying one FieldViolation per
// violation, which the Phase 5 grpc renderer converts to codes.InvalidArgument +
// errdetails.BadRequest. Routing through the single AppError path keeps the grpc
// violation shape byte-identical to fiber's invalid_params. Field paths and
// reasons are normalized to match the fiber transport (dotted path; reason = the
// rule id's final segment, e.g. "string.email" -> "email").
package grpc

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"

	"buf.build/go/protovalidate"
	pkgerrors "github.com/Vilsol/lakta/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// Validator wraps a compiled protovalidate.Validator (built once, reused per RPC).
type Validator struct {
	v protovalidate.Validator
}

// NewValidator builds a Validator. With no pre-built validator it compiles a
// default protovalidate.New(); pass an existing one to share compiled constraints.
func NewValidator(pv ...protovalidate.Validator) (*Validator, error) {
	if len(pv) > 0 && pv[0] != nil {
		return &Validator{v: pv[0]}, nil
	}
	v, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("compile protovalidate validator: %w", err)
	}
	return &Validator{v: v}, nil
}

// validate runs protovalidate over a request message. A non-proto request is
// skipped (nil); a *protovalidate.ValidationError becomes an AppError{VALIDATION};
// any other error (runtime/compilation) becomes errors.Internal.
func (val *Validator) validate(msg any) error {
	pm, ok := msg.(proto.Message)
	if !ok {
		return nil
	}

	err := val.v.Validate(pm)
	if err == nil {
		return nil
	}

	var ve *protovalidate.ValidationError
	if stderrors.As(err, &ve) {
		return toAppError(ve)
	}
	return pkgerrors.Internal("validation error").WithCause(err)
}

// UnaryServerInterceptor validates each request message before the handler runs.
func UnaryServerInterceptor(v *Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := v.validate(req); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor validates each message received on the stream.
func StreamServerInterceptor(v *Validator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &validatingStream{ServerStream: ss, v: v})
	}
}

// validatingStream validates every received message, surfacing an AppError from
// RecvMsg so the Phase 5 renderer maps it to InvalidArgument.
type validatingStream struct {
	grpc.ServerStream

	v *Validator
}

func (s *validatingStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err //nolint:wrapcheck // return RecvMsg's error verbatim so grpc's io.EOF sentinel checks still fire
	}
	return s.v.validate(m)
}

// toAppError converts a *protovalidate.ValidationError into errors.Validation with
// one WithField per CEL violation. v1.2.0: violation accessors live on .Proto.
func toAppError(ve *protovalidate.ValidationError) *pkgerrors.AppError {
	appErr := pkgerrors.Validation("validation failed")
	for _, viol := range ve.Violations {
		appErr = appErr.WithField(protovalidate.FieldPathString(viol.Proto.GetField()), reason(viol))
	}
	return appErr
}

// reason normalizes a violation's rule id to match validator/v10's tag vocabulary
// so both transports emit byte-identical reasons: the final dotted segment of the
// rule id ("string.email" -> "email"); the message is the fallback when absent.
func reason(v *protovalidate.Violation) string {
	if id := v.Proto.GetRuleId(); id != "" {
		if i := strings.LastIndexByte(id, '.'); i >= 0 {
			return id[i+1:]
		}
		return id
	}
	return v.Proto.GetMessage()
}
