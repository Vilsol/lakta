package pool_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/workers/pool"
)

func TestSubmitResult_ReturnsValue(t *testing.T) {
	t.Parallel()
	p := newPool(t, 2, 8)

	f, err := pool.SubmitResult(t.Context(), p, func(_ context.Context) (int, error) {
		return 42, nil
	})
	testza.AssertNoError(t, err)

	v, err := f.Get(t.Context())
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 42, v)
}

func TestSubmitResult_PropagatesError(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	taskErr := errors.New("task failed")
	f, err := pool.SubmitResult(t.Context(), p, func(_ context.Context) (int, error) {
		return 0, taskErr
	})
	testza.AssertNoError(t, err)

	_, err = f.Get(t.Context())
	testza.AssertErrorIs(t, err, taskErr)
}

func TestSubmitResult_PanicBecomesError(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	f, err := pool.SubmitResult(t.Context(), p, func(_ context.Context) (int, error) {
		panic("result task exploded")
	})
	testza.AssertNoError(t, err)

	_, err = f.Get(t.Context())
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "result task exploded")
}

func TestFuture_GetIsRepeatSafe(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	f, err := pool.SubmitResult(t.Context(), p, func(_ context.Context) (string, error) {
		return "once", nil
	})
	testza.AssertNoError(t, err)

	for range 3 {
		v, getErr := f.Get(t.Context())
		testza.AssertNoError(t, getErr)
		testza.AssertEqual(t, "once", v)
	}
}

func TestSubmitResult_SkipsQueuedTaskWhenCtxCancelled(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 1)

	release := make(chan struct{})
	started := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		close(started)
		<-release
		return nil
	}))
	waitFor(t, started)

	ctx, cancel := context.WithCancel(t.Context())
	ran := false
	f, err := pool.SubmitResult(ctx, p, func(_ context.Context) (int, error) {
		ran = true
		return 1, nil
	})
	testza.AssertNoError(t, err)

	cancel()
	close(release)

	_, err = f.Get(t.Context())
	testza.AssertErrorIs(t, err, context.Canceled)
	testza.AssertFalse(t, ran)
}

func TestSubmitResult_TaskSeesLiveCancellation(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	release := make(chan struct{})
	running := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())

	f, err := pool.SubmitResult(ctx, p, func(taskCtx context.Context) (error, error) {
		close(running)
		<-release
		return taskCtx.Err(), nil
	})
	testza.AssertNoError(t, err)

	waitFor(t, running)
	cancel()
	close(release)

	observed, err := f.Get(t.Context())
	testza.AssertNoError(t, err)
	testza.AssertErrorIs(t, observed, context.Canceled)
}

func TestFuture_GetRespectsWaitCtx(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	release := make(chan struct{})
	defer close(release)
	f, err := pool.SubmitResult(t.Context(), p, func(_ context.Context) (int, error) {
		<-release
		return 1, nil
	})
	testza.AssertNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = f.Get(ctx)

	testza.AssertErrorIs(t, err, context.DeadlineExceeded)
}
