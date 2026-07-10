package connect

import (
	"context"
	stderrors "errors"
	"log/slog"
	"strings"
	"sync"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	"github.com/Vilsol/slox"
	"github.com/samber/oops"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"
)

const internalMessage = "internal error"

// otelInterceptor builds the otelconnect tracing/metrics interceptor. It only
// errors on invalid options; with none it never does, so a nil result degrades
// gracefully rather than panicking in a config accessor.
func otelInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
	oi, err := otelconnect.NewInterceptor()
	if err != nil {
		return nil
	}
	return oi
}

// loggingInterceptor logs each finished unary call, mirroring grpc/server's
// logging.FinishCall event.
func loggingInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			resp, err := next(ctx, req)

			attrs := []any{slog.String("procedure", req.Spec().Procedure)}
			if err != nil {
				attrs = append(attrs, slog.String("code", connect.CodeOf(err).String()))
			}
			slox.Log(ctx, slog.LevelInfo, "finished connect call", attrs...)

			return resp, err
		}
	})
}

// recoveryInterceptor recovers a panicking handler into an opaque INTERNAL
// AppError rendered as a connect.Error. It sits outer of errorInterceptor, so it
// renders the panic itself; the panic value stays in the log line, never on the
// wire (mirrors grpc/server's recovery handler).
func recoveryInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) { //nolint:nonamedreturns // deferred recover sets err
			defer func() {
				if r := recover(); r != nil {
					slox.Error(ctx, "recovered from panic in connect handler", slog.Any("panic", r))
					err = toConnectError(apperrors.Internal(internalMessage))
				}
			}()

			return next(ctx, req)
		}
	})
}

// errorInterceptor renders any error returned by an inner handler/interceptor
// through the §5 AppError path so a returned *AppError (including the validation
// interceptor's) reaches the wire as a connect.Error carrying the same
// errdetails.ErrorInfo/BadRequest a gRPC client sees. A pre-built connect.Error
// passes through untouched.
func errorInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			resp, err := next(ctx, req)
			if err == nil {
				return resp, nil
			}

			var cerr *connect.Error
			if stderrors.As(err, &cerr) {
				return resp, err
			}

			return resp, toConnectError(apperrors.FromError(err))
		}
	})
}

// validationInterceptor runs protovalidate over each unary request message,
// mirroring pkg/validation/grpc byte-for-byte: a CEL violation becomes an
// AppError{VALIDATION} with one FieldViolation per violation (reason = the rule
// id's final dotted segment), which errorInterceptor renders to
// CodeInvalidArgument + errdetails.BadRequest — identical to the gRPC transport.
func validationInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
	var (
		once      sync.Once
		validator protovalidate.Validator
		buildErr  error
	)
	get := func() (protovalidate.Validator, error) {
		once.Do(func() {
			validator, buildErr = protovalidate.New()
			buildErr = oops.Wrapf(buildErr, "failed to compile protovalidate validator")
		})
		return validator, buildErr
	}

	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if err := validateMessage(get, req.Any()); err != nil {
				return nil, err
			}
			return next(ctx, req)
		}
	})
}

// validateMessage runs protovalidate over a request message. A non-proto request
// is skipped; a *protovalidate.ValidationError becomes an AppError{VALIDATION};
// any other error (runtime/compilation) becomes errors.Internal.
func validateMessage(get func() (protovalidate.Validator, error), msg any) error {
	pm, ok := msg.(proto.Message)
	if !ok {
		return nil
	}

	v, err := get()
	if err != nil {
		return apperrors.Internal("validation error").WithCause(err)
	}

	verr := v.Validate(pm)
	if verr == nil {
		return nil
	}

	var ve *protovalidate.ValidationError
	if stderrors.As(verr, &ve) {
		return toValidationAppError(ve)
	}
	return apperrors.Internal("validation error").WithCause(verr)
}

// toValidationAppError converts a *protovalidate.ValidationError into
// errors.Validation with one WithField per CEL violation (mirrors valgrpc).
func toValidationAppError(ve *protovalidate.ValidationError) *apperrors.AppError {
	appErr := apperrors.Validation("validation failed")
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

// toConnectError renders an *AppError as a connect.Error: connect.Code cast from
// AppError.GRPC (shared canonical numbering), errdetails.ErrorInfo{Reason,
// Metadata} always, and errdetails.BadRequest when Fields is non-empty. INTERNAL
// renders the opaque message.
func toConnectError(e *apperrors.AppError) *connect.Error {
	message := e.Message
	if e.Code == apperrors.CodeInternal {
		message = internalMessage
	}

	cerr := connect.NewError(connect.Code(e.GRPC), stderrors.New(message))

	if info, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: string(e.Code), Metadata: e.Meta}); err == nil {
		cerr.AddDetail(info)
	}

	if len(e.Fields) > 0 {
		violations := make([]*errdetails.BadRequest_FieldViolation, 0, len(e.Fields))
		for _, f := range e.Fields {
			violations = append(violations, &errdetails.BadRequest_FieldViolation{
				Field:       f.Field,
				Description: f.Description,
			})
		}
		if br, err := connect.NewErrorDetail(&errdetails.BadRequest{FieldViolations: violations}); err == nil {
			cerr.AddDetail(br)
		}
	}

	return cerr
}
