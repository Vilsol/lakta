package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/events/bus"
)

type ctxKey struct{}

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

func TestSubscribeAsync_DeliversEvent(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	got := make(chan userCreated, 1)
	bus.SubscribeAsync(b, func(_ context.Context, e userCreated) error {
		got <- e
		return nil
	})

	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 42}))

	testza.AssertEqual(t, 42, waitFor(t, got).ID)
}

func TestSubscribeAsync_PreservesFIFOOrder(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	const total = 100
	got := make(chan int, total)
	bus.SubscribeAsync(b, func(_ context.Context, e userCreated) error {
		got <- e.ID
		return nil
	})

	for i := range total {
		testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: i}))
	}

	for i := range total {
		testza.AssertEqual(t, i, waitFor(t, got))
	}
}

func TestSubscribeAsync_HandlerCtxSurvivesPublisherCancel(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	release := make(chan struct{})
	result := make(chan error, 1)
	value := make(chan any, 1)
	bus.SubscribeAsync(b, func(ctx context.Context, _ userCreated) error {
		<-release
		result <- ctx.Err()
		value <- ctx.Value(ctxKey{})
		return nil
	})

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxKey{}, "propagated"))
	testza.AssertNoError(t, bus.Publish(ctx, b, userCreated{ID: 1}))
	cancel()
	close(release)

	testza.AssertNoError(t, waitFor(t, result))
	testza.AssertEqual(t, "propagated", waitFor(t, value))
}

func TestSubscribeAsync_HandlerPanicDoesNotKillSubscriber(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	got := make(chan int, 1)
	bus.SubscribeAsync(b, func(_ context.Context, e userCreated) error {
		if e.ID == 1 {
			panic("first event explodes")
		}
		got <- e.ID
		return nil
	})

	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 1}))
	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 2}))

	testza.AssertEqual(t, 2, waitFor(t, got))
}

func TestPublish_ReturnsCtxErrWhenQueueFullAndCtxCancelled(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(1)

	release := make(chan struct{})
	started := make(chan struct{})
	bus.SubscribeAsync(b, func(_ context.Context, _ userCreated) error {
		close(started)
		<-release
		return nil
	})
	defer close(release)

	// First event occupies the handler, second fills the queue.
	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 1}))
	waitFor(t, started)
	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 2}))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := bus.Publish(ctx, b, userCreated{ID: 3})

	testza.AssertErrorIs(t, err, context.DeadlineExceeded)
}

func TestPublish_UnblocksWhenConsumerDrains(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(1)

	release := make(chan struct{})
	started := make(chan struct{})
	bus.SubscribeAsync(b, func(_ context.Context, _ userCreated) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})

	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 1}))
	waitFor(t, started)
	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 2}))

	published := make(chan error, 1)
	go func() {
		published <- bus.Publish(t.Context(), b, userCreated{ID: 3})
	}()

	select {
	case <-published:
		t.Fatal("publish should block while queue is full")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	testza.AssertNoError(t, waitFor(t, published))
}

func TestUnsubscribeAsync_StopsDelivery(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	got := make(chan int, 2)
	unsubscribe := bus.SubscribeAsync(b, func(_ context.Context, e userCreated) error {
		got <- e.ID
		return nil
	})

	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 1}))
	testza.AssertEqual(t, 1, waitFor(t, got))

	unsubscribe()
	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 2}))

	select {
	case id := <-got:
		t.Fatalf("received event %d after unsubscribe", id)
	case <-time.After(100 * time.Millisecond):
	}
}
