package bus_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/events/bus"
)

type userCreated struct {
	ID int
}

type orderPlaced struct {
	ID int
}

func TestPublish_DeliversToSyncSubscriber(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	var got userCreated
	bus.Subscribe(b, func(_ context.Context, e userCreated) error {
		got = e
		return nil
	})

	err := bus.Publish(t.Context(), b, userCreated{ID: 42})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 42, got.ID)
}

func TestPublish_DeliversToAllSyncSubscribers(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	calls := 0
	bus.Subscribe(b, func(_ context.Context, _ userCreated) error {
		calls++
		return nil
	})
	bus.Subscribe(b, func(_ context.Context, _ userCreated) error {
		calls++
		return nil
	})

	err := bus.Publish(t.Context(), b, userCreated{ID: 1})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 2, calls)
}

func TestPublish_JoinsSyncHandlerErrors(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	errA := errors.New("handler a failed")
	errB := errors.New("handler b failed")
	bus.Subscribe(b, func(_ context.Context, _ userCreated) error { return errA })
	bus.Subscribe(b, func(_ context.Context, _ userCreated) error { return errB })

	err := bus.Publish(t.Context(), b, userCreated{ID: 1})

	testza.AssertTrue(t, errors.Is(err, errA))
	testza.AssertTrue(t, errors.Is(err, errB))
}

func TestPublish_DistinctTypesAreDistinctTopics(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	calls := 0
	bus.Subscribe(b, func(_ context.Context, _ userCreated) error {
		calls++
		return nil
	})

	err := bus.Publish(t.Context(), b, orderPlaced{ID: 1})

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 0, calls)
}

func TestUnsubscribe_StopsDelivery(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	calls := 0
	unsubscribe := bus.Subscribe(b, func(_ context.Context, _ userCreated) error {
		calls++
		return nil
	})

	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 1}))
	unsubscribe()
	testza.AssertNoError(t, bus.Publish(t.Context(), b, userCreated{ID: 2}))

	testza.AssertEqual(t, 1, calls)
}

func TestPublish_SyncHandlerPanicBecomesError(t *testing.T) {
	t.Parallel()
	b := bus.NewBus(0)

	bus.Subscribe(b, func(_ context.Context, _ userCreated) error {
		panic("handler exploded")
	})

	err := bus.Publish(t.Context(), b, userCreated{ID: 1})

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "handler exploded")
}
