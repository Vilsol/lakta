// Package grpc renders lakta AppErrors as gRPC status + errdetails. Import it
// aliased (e.g. errgrpc) to avoid clashing with google.golang.org/grpc.
package grpc

import (
	"context"

	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
)

const internalMessage = "internal error"

// UnaryServerInterceptor renders a handler's returned error via errors.FromError
// into a *status.Status: GRPC code + Message, attaching errdetails.ErrorInfo
// {Reason: string(Code), Metadata: Meta} and, when Fields is non-empty,
// errdetails.BadRequest{FieldViolations}. Attaches via status.WithDetails.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		if err == nil {
			return resp, nil
		}
		return resp, toStatus(errpkg.FromError(err)).Err()
	}
}

// StreamServerInterceptor is the streaming equivalent.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		if err == nil {
			return nil
		}
		return toStatus(errpkg.FromError(err)).Err()
	}
}

// toStatus builds a *status.Status from an *AppError, attaching ErrorInfo and
// (conditionally) BadRequest details. INTERNAL renders the opaque message.
func toStatus(e *errpkg.AppError) *status.Status {
	message := e.Message
	if e.Code == errpkg.CodeInternal {
		message = internalMessage
	}

	st := status.New(e.GRPC, message)

	details := []protoadapt.MessageV1{
		&errdetails.ErrorInfo{Reason: string(e.Code), Metadata: e.Meta},
	}
	if len(e.Fields) > 0 {
		violations := make([]*errdetails.BadRequest_FieldViolation, 0, len(e.Fields))
		for _, f := range e.Fields {
			violations = append(violations, &errdetails.BadRequest_FieldViolation{
				Field:       f.Field,
				Description: f.Description,
			})
		}
		details = append(details, &errdetails.BadRequest{FieldViolations: violations})
	}

	withDetails, err := st.WithDetails(details...)
	if err != nil {
		return st
	}
	return withDetails
}
