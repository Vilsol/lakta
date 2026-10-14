package connect

import (
	"context"
	"time"

	"connectrpc.com/connect"
)

// deadlineInterceptor gives unary calls without a client deadline a default
// one and caps client deadlines. Streams are left alone: they are long-lived
// by design and bounded by the client.
type deadlineInterceptor struct {
	defaultDeadline time.Duration
	maxDeadline     time.Duration
}

func (d deadlineInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, cancel := d.apply(ctx)
		defer cancel()
		return next(ctx, req)
	}
}

func (deadlineInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (deadlineInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (d deadlineInterceptor) apply(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		if d.maxDeadline > 0 && time.Until(deadline) > d.maxDeadline {
			return context.WithTimeout(ctx, d.maxDeadline)
		}
		return ctx, func() {}
	}
	if d.defaultDeadline > 0 {
		return context.WithTimeout(ctx, d.defaultDeadline)
	}
	return ctx, func() {}
}
