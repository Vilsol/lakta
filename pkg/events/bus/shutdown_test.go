package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/events/bus"
)

func TestClose_DrainsQueuedAsyncEvents(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(8)

	release := make(chan struct{})
	got := make(chan int, 3)
	bus.SubscribeAsync(b, func(_ context.Context, e userCreated) error {
		<-release
		got <- e.ID
		return nil
	})

	for i := range 3 {
		testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: i}))
	}

	closed := make(chan error, 1)
	go func() {
		closed <- b.Close(context.Background())
	}()
	close(release)

	testza.AssertNoError(t, waitFor(t, closed))
	// The drain contract: all queued events are delivered before Close returns.
	for i := range 3 {
		select {
		case id := <-got:
			testza.AssertEqual(t, i, id)
		default:
			t.Fatalf("event %d not delivered before Close returned", i)
		}
	}
}

func TestPublish_AfterCloseReturnsErrBusClosed(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	testza.AssertNoError(t, b.Close(t.Context()))

	err := bus.Publish(t.Context(), b, userCreated{ID: 1})
	testza.AssertErrorIs(t, err, bus.ErrBusClosed)
}

func TestClose_ReturnsCtxErrWhenHandlerStuck(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(1)

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	bus.SubscribeAsync(b, func(_ context.Context, _ userCreated) error {
		close(started)
		<-release
		return nil
	})

	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 1}))
	waitFor(t, started)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := b.Close(ctx)

	testza.AssertErrorIs(t, err, context.DeadlineExceeded)
}
