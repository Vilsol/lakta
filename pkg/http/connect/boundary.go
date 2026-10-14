package connect

import (
	"context"
	stderrors "errors"
	"log/slog"

	"connectrpc.com/connect"
	apperrors "github.com/Vilsol/lakta/pkg/errors"
	"github.com/Vilsol/slox"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// errorBoundaryInterceptor is the single place a failed call is rendered,
// logged and recorded. Everything inner (recovery, deadline, auth, authz,
// validation, handler) returns plain errors; the boundary maps them to the
// wire via apperrors.FromError so no oops context, stack or cause leaks.
type errorBoundaryInterceptor struct{}

func (errorBoundaryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)
		if err != nil {
			return nil, renderError(ctx, req.Spec().Procedure, err)
		}
		return resp, nil
	}
}

func (errorBoundaryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (errorBoundaryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := next(ctx, conn); err != nil {
			return renderError(ctx, conn.Spec().Procedure, err)
		}
		return nil
	}
}

func renderError(ctx context.Context, procedure string, err error) error {
	app := classify(err)
	if app == nil {
		// A handler-built *connect.Error is already wire-shaped: pass it through.
		var cerr *connect.Error
		stderrors.As(err, &cerr)
		logFailure(ctx, procedure, err, cerr.Code().String(), "", serverClass(cerr.Code()))
		return err
	}

	logFailure(ctx, procedure, err, app.ReasonOrCode(), app.Domain, app.HTTP >= 500) //nolint:mnd // 5xx boundary
	recordSpan(ctx, err, app)

	return toConnectError(app)
}

// classify maps err to an AppError, or returns nil for a bare *connect.Error
// that carries no oops/AppError underneath.
func classify(err error) *apperrors.AppError {
	switch {
	case stderrors.Is(err, context.DeadlineExceeded):
		return apperrors.New(apperrors.CodeDeadlineExceeded, "deadline exceeded").WithCause(err)
	case stderrors.Is(err, context.Canceled):
		return apperrors.New(apperrors.CodeAborted, "canceled").WithCause(err)
	}

	var appErr *apperrors.AppError
	if _, isOops := oops.AsOops(err); !isOops && !stderrors.As(err, &appErr) {
		var cerr *connect.Error
		if stderrors.As(err, &cerr) {
			return nil
		}
	}

	return apperrors.FromError(err)
}

func logFailure(ctx context.Context, procedure string, err error, code, domain string, server bool) {
	level := slog.LevelWarn
	if server {
		level = slog.LevelError
	}
	slox.Log(ctx, level, "connect call failed",
		slog.String("procedure", procedure),
		slog.String("error_code", code),
		slog.String("error_domain", domain),
		slog.Any("error", err))
}

func recordSpan(ctx context.Context, err error, app *apperrors.AppError) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("error.code", app.ReasonOrCode()),
		attribute.String("error.domain", app.Domain),
	}
	if oopsErr, ok := oops.AsOops(err); ok {
		attrs = append(attrs, attribute.String("exception.stacktrace", oopsErr.Stacktrace()))
	}
	span.RecordError(err, trace.WithAttributes(attrs...))

	if app.HTTP >= 500 { //nolint:mnd // 5xx boundary
		span.SetStatus(codes.Error, app.ReasonOrCode())
	}
}

func serverClass(code connect.Code) bool {
	switch code {
	case connect.CodeUnknown, connect.CodeInternal, connect.CodeUnavailable, connect.CodeDataLoss,
		connect.CodeDeadlineExceeded, connect.CodeUnimplemented:
		return true
	default:
		return false
	}
}
