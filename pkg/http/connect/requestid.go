package connect

import (
	"context"
	stderrors "errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/Vilsol/slox"
	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

// RequestIDHeader carries the request ID in requests and responses.
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// RequestID returns the request ID of the current call, or "" outside one.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestIDInterceptor accepts an inbound X-Request-Id only when it is a UUID
// or ULID (anything else could smuggle log-injection payloads), otherwise
// generates a ULID. The ID goes into ctx, the ctx logger and the response.
type requestIDInterceptor struct{}

func (requestIDInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		id := resolveRequestID(req.Header().Get(RequestIDHeader))
		resp, err := next(withRequestID(ctx, id), req)
		if resp != nil {
			resp.Header().Set(RequestIDHeader, id)
		}
		var cerr *connect.Error
		if stderrors.As(err, &cerr) {
			cerr.Meta().Set(RequestIDHeader, id)
		}
		return resp, err
	}
}

func (requestIDInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (requestIDInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		id := resolveRequestID(conn.RequestHeader().Get(RequestIDHeader))
		conn.ResponseHeader().Set(RequestIDHeader, id)
		return next(withRequestID(ctx, id), conn)
	}
}

func withRequestID(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, requestIDKey{}, id)
	return slox.With(ctx, slog.String("request_id", id))
}

func resolveRequestID(inbound string) string {
	if validRequestID(inbound) {
		return inbound
	}
	return ulid.Make().String()
}

func validRequestID(s string) bool {
	if len(s) == 36 { //nolint:mnd // canonical UUID text length
		_, err := uuid.Parse(s)
		return err == nil
	}
	_, err := ulid.ParseStrict(s)
	return err == nil
}
