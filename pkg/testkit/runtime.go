package testkit

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Vilsol/lakta/pkg/lakta"
)

// naturalCompletionGrace is how long Shutdown waits for RunContext to finish on its own
// before sending a cancellation signal. Errors from init or start return in microseconds,
// so 100ms is ample on any hardware.
const naturalCompletionGrace = 100 * time.Millisecond

// RuntimeHarness starts a Runtime in a goroutine and provides a way to trigger graceful shutdown.
type RuntimeHarness struct {
	t      *testing.T
	once   sync.Once
	cancel context.CancelFunc
	done   chan error
	err    error
}

// NewRuntimeHarness creates a Runtime with the given modules and starts it immediately.
// t.Cleanup ensures Shutdown is called if the test does not call it explicitly.
func NewRuntimeHarness(t *testing.T, modules ...lakta.Module) *RuntimeHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	h := &RuntimeHarness{
		t:      t,
		cancel: cancel,
		done:   make(chan error, 1),
	}

	rt := lakta.NewRuntime(modules...)
	go func() {
		h.done <- rt.RunContext(ctx)
	}()

	t.Cleanup(func() {
		_ = h.Shutdown()
	})

	return h
}

// Shutdown waits for RunContext to complete naturally, then cancels the context if it hasn't yet.
// Safe to call multiple times; only the first call has effect.
func (h *RuntimeHarness) Shutdown() error {
	h.once.Do(func() {
		// Give RunContext a chance to complete on its own (e.g. after init/start error or
		// after all non-blocking starts complete). This avoids a race where our cancel()
		// preempts a natural completion via startDone in the runtime's select.
		select {
		case h.err = <-h.done:
			h.cancel()
		case <-time.After(naturalCompletionGrace):
			h.cancel()
			h.err = <-h.done
		}
	})
	return h.err
}

// Err returns the RunContext error without blocking (only valid after Shutdown).
func (h *RuntimeHarness) Err() error {
	return h.err
}

const (
	waitForAddrTimeout  = 2 * time.Second
	waitForAddrInterval = 10 * time.Millisecond
)

// addrProvider is implemented by server modules that expose their bound listener address.
type addrProvider interface {
	Addr() net.Addr
}

// WaitForAddr polls until m.Addr() returns a non-nil value or times out after 2 seconds.
// Used in server module tests to wait for the listener to be bound before making connections.
func WaitForAddr(t *testing.T, m addrProvider) net.Addr {
	t.Helper()
	deadline := time.Now().Add(waitForAddrTimeout)
	for time.Now().Before(deadline) {
		if addr := m.Addr(); addr != nil {
			return addr
		}
		time.Sleep(waitForAddrInterval)
	}
	t.Fatal("timed out waiting for server address")
	return nil
}
