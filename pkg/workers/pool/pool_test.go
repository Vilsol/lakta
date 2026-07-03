package pool_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/workers/pool"
)

type ctxKey struct{}

func newPool(t *testing.T, workers int, queueSize int) *pool.Pool {
	t.Helper()
	p := pool.New(pool.PoolConfig{Workers: workers, QueueSize: new(queueSize)})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p
}

func waitFor[T any](t *testing.T, ch <-chan T) T { //nolint:ireturn // generic helper returns the received value
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting on channel")
		panic("unreachable")
	}
}

func TestSubmit_ExecutesTask(t *testing.T) {
	t.Parallel()
	p := newPool(t, 2, 8)

	done := make(chan int, 1)
	err := p.Submit(t.Context(), func(_ context.Context) error {
		done <- 42
		return nil
	})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 42, waitFor(t, done))
}

func TestSubmit_TaskCtxSurvivesSubmitterCancel(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	release := make(chan struct{})
	ctxErr := make(chan error, 1)
	value := make(chan any, 1)
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "propagated"))

	testza.AssertNoError(t, p.Submit(ctx, func(taskCtx context.Context) error {
		<-release
		ctxErr <- taskCtx.Err()
		value <- taskCtx.Value(ctxKey{})
		return nil
	}))
	cancel()
	close(release)

	testza.AssertNoError(t, waitFor(t, ctxErr))
	testza.AssertEqual(t, "propagated", waitFor(t, value))
}

func TestSubmit_TaskErrorDoesNotAffectPool(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		return errors.New("task failed")
	}))

	done := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		close(done)
		return nil
	}))
	waitFor(t, done)
}

func TestSubmit_PanicRecoveredPoolKeepsWorking(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 8)

	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		panic("task exploded")
	}))

	done := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		close(done)
		return nil
	}))
	waitFor(t, done)
}

func TestSubmit_ReturnsCtxErrWhenQueueFullAndCtxCancelled(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 1)

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		close(started)
		<-release
		return nil
	}))
	waitFor(t, started)
	// Worker is busy; this one fills the queue.
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error { return nil }))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := p.Submit(ctx, func(_ context.Context) error { return nil })

	testza.AssertErrorIs(t, err, context.DeadlineExceeded)
}

func TestSubmit_UnblocksWhenWorkerDrains(t *testing.T) {
	t.Parallel()
	p := newPool(t, 1, 1)

	release := make(chan struct{})
	started := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	}))
	waitFor(t, started)
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error { return nil }))

	submitted := make(chan error, 1)
	go func() {
		submitted <- p.Submit(t.Context(), func(_ context.Context) error { return nil })
	}()

	select {
	case <-submitted:
		t.Fatal("submit should block while queue is full")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	testza.AssertNoError(t, waitFor(t, submitted))
}

func TestClose_DrainsQueuedTasksBeforeReturning(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.PoolConfig{Workers: 1, QueueSize: new(8)})

	release := make(chan struct{})
	ran := make(chan int, 3)
	for i := range 3 {
		testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
			<-release
			ran <- i
			return nil
		}))
	}

	closed := make(chan error, 1)
	go func() {
		closed <- p.Close(context.Background())
	}()
	close(release)

	testza.AssertNoError(t, waitFor(t, closed))
	// The drain contract: all queued tasks ran before Close returned.
	for range 3 {
		select {
		case <-ran:
		default:
			t.Fatal("queued task not executed before Close returned")
		}
	}
}

func TestSubmit_AfterCloseReturnsErrPoolClosed(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.PoolConfig{Workers: 1, QueueSize: new(1)})

	testza.AssertNoError(t, p.Close(t.Context()))

	err := p.Submit(t.Context(), func(_ context.Context) error { return nil })
	testza.AssertErrorIs(t, err, pool.ErrPoolClosed)
}

func TestClose_ReturnsCtxErrWhenTaskStuck(t *testing.T) {
	t.Parallel()
	p := pool.New(pool.PoolConfig{Workers: 1, QueueSize: new(1)})

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		close(started)
		<-release
		return nil
	}))
	waitFor(t, started)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := p.Close(ctx)

	testza.AssertErrorIs(t, err, context.DeadlineExceeded)
}
