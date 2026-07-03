package pool

import (
	"context"

	"github.com/samber/oops"
	"github.com/sourcegraph/conc/panics"
)

// Future holds the eventual result of a task submitted via [SubmitResult].
type Future[T any] struct {
	done chan struct{}
	val  T
	err  error
}

// SubmitResult enqueues a task whose result the caller awaits via the
// returned Future. Unlike Submit, the task keeps the submitter's live
// context: cancellation propagates, and a task still queued when ctx is
// cancelled is skipped. Panics surface as errors from Get.
func SubmitResult[T any](ctx context.Context, p *Pool, fn func(ctx context.Context) (T, error)) (*Future[T], error) {
	f := &Future[T]{done: make(chan struct{})}

	err := p.enqueue(ctx, task{
		ctx: ctx,
		// Errors and panics are delivered through the future, so the task
		// itself never returns an error for the pool to log.
		run: func(taskCtx context.Context) error {
			defer close(f.done)
			if err := taskCtx.Err(); err != nil {
				f.err = oops.Wrapf(err, "task cancelled before execution")
				return nil
			}
			recovered := panics.Try(func() {
				f.val, f.err = fn(taskCtx)
			})
			if recovered != nil {
				f.err = oops.Errorf("task panicked: %s", recovered.String())
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Get blocks until the task completes or ctx is done. Safe to call
// repeatedly and from multiple goroutines.
func (f *Future[T]) Get(ctx context.Context) (T, error) { //nolint:ireturn // generic accessor returns the task result
	select {
	case <-f.done:
		return f.val, f.err
	case <-ctx.Done():
		var zero T
		return zero, oops.Wrapf(ctx.Err(), "context done while awaiting task result")
	}
}
