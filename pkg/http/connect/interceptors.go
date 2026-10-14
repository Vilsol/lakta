package connect

import (
	"context"
	stderrors "errors"
	"log/slog"
	"strings"
	"sync"
	"time"

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

// otelInterceptor builds the otelconnect tracing/metrics interceptor. Remote
// span contexts are trusted: callers are authenticated peers, so their traces
// continue rather than start new roots. It only errors on invalid options; a
// nil result degrades gracefully.
func otelInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
	oi, err := otelconnect.NewInterceptor(otelconnect.WithTrustRemote())
	if err != nil {
		return nil
	}
	return oi
}

// handlerInterceptor adapts a pair of server-side wrappers to connect.Interceptor;
// client streams pass through untouched (server chain only).
type handlerInterceptor struct {
	unary  func(connect.UnaryFunc) connect.UnaryFunc
	stream func(connect.StreamingHandlerFunc) connect.StreamingHandlerFunc
}

func (h handlerInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return h.unary(next) }

func (h handlerInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (h handlerInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return h.stream(next)
}

// accessLogInterceptor logs each finished call with its procedure, code and
// duration. It sits outside the error boundary, so it sees rendered codes.
type accessLogInterceptor struct{}

func (accessLogInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		logFinished(ctx, req.Spec().Procedure, start, err)
		return resp, err
	}
}

func (accessLogInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (accessLogInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()
		err := next(ctx, conn)
		logFinished(ctx, conn.Spec().Procedure, start, err)
		return err
	}
}

func logFinished(ctx context.Context, procedure string, start time.Time, err error) {
	code := "ok"
	if err != nil {
		code = connect.CodeOf(err).String()
	}
	slox.Info(ctx, "finished connect call",
		slog.String("procedure", procedure),
		slog.String("code", code),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()))
}

// recoveryInterceptor turns a handler panic into an oops error carrying the
// panic value and stack. The error boundary (outer) renders it as an opaque
// INTERNAL and logs the details; nothing of the panic reaches the wire.
type recoveryInterceptor struct{}

func (recoveryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		var (
			resp connect.AnyResponse
			err  error
		)
		if perr := oops.Recoverf(func() { resp, err = next(ctx, req) }, "panic in connect handler"); perr != nil {
			return nil, perr //nolint:wrapcheck // already an oops error carrying the panic stack
		}
		return resp, err
	}
}

func (recoveryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (recoveryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		var err error
		if perr := oops.Recoverf(func() { err = next(ctx, conn) }, "panic in connect handler"); perr != nil {
			return perr //nolint:wrapcheck // already an oops error carrying the panic stack
		}
		return err
	}
}

// newValidationInterceptor runs protovalidate over every request message, unary
// and streamed, mirroring pkg/validation/grpc: a CEL violation becomes an
// AppError{VALIDATION} with one FieldViolation per violation, which the error
// boundary renders to CodeInvalidArgument + errdetails.BadRequest.
func newValidationInterceptor() connect.Interceptor { //nolint:ireturn // connect.Interceptor is the library's composition unit
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

	return handlerInterceptor{
		unary: func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				if err := validateMessage(get, req.Any()); err != nil {
					return nil, err
				}
				return next(ctx, req)
			}
		},
		stream: func(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
			return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
				return next(ctx, &validatingConn{StreamingHandlerConn: conn, get: get})
			}
		},
	}
}

// validatingConn validates each received message of a streaming call.
type validatingConn struct {
	connect.StreamingHandlerConn

	get func() (protovalidate.Validator, error)
}

func (c *validatingConn) Receive(msg any) error {
	if err := c.StreamingHandlerConn.Receive(msg); err != nil {
		return err //nolint:wrapcheck // io.EOF must reach the handler unwrapped
	}
	return validateMessage(c.get, msg)
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
// Domain, Metadata} always, and errdetails.BadRequest when Fields is non-empty.
// INTERNAL renders the opaque message.
func toConnectError(e *apperrors.AppError) *connect.Error {
	message := e.Message
	if e.Code == apperrors.CodeInternal {
		message = internalMessage
	}

	cerr := connect.NewError(connect.Code(e.GRPC), stderrors.New(message))

	if info, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: e.ReasonOrCode(), Domain: e.Domain, Metadata: e.Meta}); err == nil {
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
