package bus

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"sync"

	"github.com/Vilsol/slox"
	"github.com/samber/oops"
)

const defaultBufferSize = 1024

// Bus is an in-process typed event bus. The Go type of a published event is
// its topic: subscribers to T receive exactly the events published as T.
type Bus struct {
	mu         sync.RWMutex
	subs       map[reflect.Type][]*subscription
	bufferSize int
	closed     bool
}

// Handler processes a published event.
type Handler[T any] func(ctx context.Context, event T) error

type queuedEvent struct {
	ctx   context.Context //nolint:containedctx // carries the publisher's detached value context per event
	event any
}

type subscription struct {
	invoke   func(ctx context.Context, event any) error
	queue    chan queuedEvent // nil for sync subscriptions
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// ErrBusClosed is returned by Publish after the bus has been closed.
var ErrBusClosed = errors.New("event bus is closed")

// NewBus creates a new event bus. A bufferSize of 0 or less uses the default
// async queue capacity.
func NewBus(bufferSize int) *Bus {
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	return &Bus{
		subs:       make(map[reflect.Type][]*subscription),
		bufferSize: bufferSize,
	}
}

// Close stops intake and drains queued async events, bounded by ctx.
// Publish returns [ErrBusClosed] afterwards. Close is idempotent.
func (b *Bus) Close(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var subs []*subscription
	for _, list := range b.subs {
		subs = append(subs, list...)
	}
	b.subs = make(map[reflect.Type][]*subscription)
	b.mu.Unlock()

	// Signal all subscribers first so they drain concurrently, then wait.
	for _, sub := range subs {
		if sub.queue != nil {
			sub.signalStop()
		}
	}
	for _, sub := range subs {
		if sub.queue == nil {
			continue
		}
		select {
		case <-sub.done:
		case <-ctx.Done():
			return oops.Wrapf(ctx.Err(), "timed out draining event bus")
		}
	}
	return nil
}

// Subscribe registers fn to run synchronously in the publisher's goroutine.
// Handler errors are joined into Publish's return value. The returned
// function unsubscribes.
func Subscribe[T any](bus *Bus, fn Handler[T]) func() {
	return bus.add(reflect.TypeFor[T](), &subscription{
		invoke: invoker(fn),
	})
}

// SubscribeAsync registers fn to run on a dedicated goroutine fed by a bounded
// FIFO queue. Handler errors are logged, not returned to publishers. The
// handler context is detached from the publisher's cancellation but keeps its
// values (trace IDs, logger, injector). The returned function unsubscribes,
// draining already-queued events first.
func SubscribeAsync[T any](bus *Bus, fn Handler[T]) func() {
	sub := &subscription{
		invoke: invoker(fn),
		queue:  make(chan queuedEvent, bus.bufferSize),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go sub.run()

	remove := bus.add(reflect.TypeFor[T](), sub)
	return func() {
		remove()
		sub.shutdown()
	}
}

// Publish delivers event to all subscribers of type T. Sync handlers run in
// the caller's goroutine; async handlers are enqueued, blocking while a
// subscriber's queue is full until ctx is done.
func Publish[T any](ctx context.Context, bus *Bus, event T) error {
	bus.mu.RLock()
	if bus.closed {
		bus.mu.RUnlock()
		return ErrBusClosed
	}
	subs := slices.Clone(bus.subs[reflect.TypeFor[T]()])
	bus.mu.RUnlock()

	var errs []error
	for _, sub := range subs {
		if sub.queue == nil {
			if err := sub.invoke(ctx, event); err != nil {
				errs = append(errs, err)
			}
			continue
		}

		select {
		case sub.queue <- queuedEvent{ctx: context.WithoutCancel(ctx), event: event}:
		case <-sub.stop:
			// Subscriber is going away; nothing left to deliver to.
		case <-ctx.Done():
			errs = append(errs, oops.Wrapf(ctx.Err(), "failed to enqueue %T", event))
		}
	}
	return errors.Join(errs...)
}

func (b *Bus) add(topic reflect.Type, sub *subscription) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], sub)

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.subs[topic] = slices.DeleteFunc(slices.Clone(b.subs[topic]), func(s *subscription) bool {
			return s == sub
		})
	}
}

// run consumes the queue until stopped, then drains whatever is left.
func (s *subscription) run() {
	defer close(s.done)
	for {
		select {
		case q := <-s.queue:
			s.deliver(q)
		case <-s.stop:
			for {
				select {
				case q := <-s.queue:
					s.deliver(q)
				default:
					return
				}
			}
		}
	}
}

func (s *subscription) deliver(q queuedEvent) {
	if err := s.invoke(q.ctx, q.event); err != nil {
		slox.Error(q.ctx, "async event handler failed",
			slog.String("event_type", reflect.TypeOf(q.event).String()),
			slog.Any("error", err),
		)
	}
}

func (s *subscription) signalStop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *subscription) shutdown() {
	s.signalStop()
	<-s.done
}

func invoker[T any](fn Handler[T]) func(ctx context.Context, event any) error {
	return func(ctx context.Context, event any) error {
		return callHandler(ctx, fn, event.(T)) //nolint:forcetypeassert // registry is keyed by T
	}
}

func callHandler[T any](ctx context.Context, fn Handler[T], event T) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = oops.Errorf("event handler panicked: %v", r)
		}
	}()
	return fn(ctx, event)
}
