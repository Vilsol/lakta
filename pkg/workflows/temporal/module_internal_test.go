package temporal

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Provider     = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

type fakeWorker struct{ stopped atomic.Bool }

func (f *fakeWorker) Stop() { f.stopped.Store(true) }

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}

func TestModule_Provides_ClientType(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, []reflect.Type{reflect.TypeFor[client.Client]()}, NewModule().Provides())
}

func TestInit_DoesNotDial(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithTarget("127.0.0.1:1"), WithTaskQueue("q"))

	start := time.Now()
	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertTrue(t, time.Since(start) < time.Second)
	testza.AssertNotNil(t, m.client)
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestInit_RegistrarErrorFails(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithTarget("127.0.0.1:1"), WithTaskQueue("q"), WithRegistrar(func(context.Context, worker.Worker) error {
		return errors.New("bad registration")
	}))
	testza.AssertNotNil(t, m.Init(h.Ctx()))
}

func TestStart_RetriesUntilWorkerStartsThenReadyFollowsHealth(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithTarget("127.0.0.1:1"), WithTaskQueue("q"), withHealthInterval(20*time.Millisecond), withBackoff(10*time.Millisecond))
	testza.AssertNil(t, m.Init(h.Ctx()))

	var attempts atomic.Int32
	var healthy atomic.Bool
	healthy.Store(true)
	w := &fakeWorker{}
	m.startWorker = func(context.Context, func(error)) (stopper, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("temporal unreachable")
		}
		return w, nil
	}
	m.checkHealth = func(context.Context) error {
		if healthy.Load() {
			return nil
		}
		return errors.New("unhealthy")
	}

	ctx, cancel := context.WithCancel(h.Ctx())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()

	testza.AssertNotNil(t, m.Ready(ctx))
	eventually(t, func() bool { return m.Ready(ctx) == nil })
	testza.AssertEqual(t, int64(2), m.failures.Load())

	healthy.Store(false)
	eventually(t, func() bool { return m.Ready(ctx) != nil })
	healthy.Store(true)
	eventually(t, func() bool { return m.Ready(ctx) == nil })

	cancel()
	testza.AssertNil(t, <-done)
	testza.AssertTrue(t, w.stopped.Load())
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestStart_DisabledBlocksUntilCancelled(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithEnabled(false))
	testza.AssertNil(t, m.Init(h.Ctx()))

	ctx, cancel := context.WithCancel(h.Ctx())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()

	select {
	case <-done:
		t.Fatal("disabled Start returned before cancellation")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	testza.AssertNil(t, <-done)
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestShutdown_WithoutStartDoesNotHang(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithTarget("127.0.0.1:1"), WithTaskQueue("q"))
	testza.AssertNil(t, m.Init(h.Ctx()))

	start := time.Now()
	testza.AssertNil(t, m.Shutdown(context.Background()))
	testza.AssertTrue(t, time.Since(start) < time.Second)
}

func TestStart_FatalWorkerErrorMakesNotReadyAndRestarts(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := NewModule(WithTarget("127.0.0.1:1"), WithTaskQueue("q"), withHealthInterval(20*time.Millisecond), withBackoff(10*time.Millisecond))
	testza.AssertNil(t, m.Init(h.Ctx()))

	var attempts atomic.Int32
	fatal := make(chan func(error), 2)
	workers := make(chan *fakeWorker, 2)
	m.startWorker = func(_ context.Context, onFatal func(error)) (stopper, error) {
		attempts.Add(1)
		w := &fakeWorker{}
		fatal <- onFatal
		workers <- w
		return w, nil
	}
	m.checkHealth = func(context.Context) error { return nil }

	ctx, cancel := context.WithCancel(h.Ctx())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()

	eventually(t, func() bool { return m.Ready(ctx) == nil })
	first := <-workers
	(<-fatal)(errors.New("namespace tmplsvc not found"))

	eventually(t, func() bool { return attempts.Load() == 2 })
	testza.AssertTrue(t, first.stopped.Load(), "the dead worker is stopped before restarting")
	eventually(t, func() bool { return m.Ready(ctx) == nil })

	cancel()
	testza.AssertNil(t, <-done)
	testza.AssertNil(t, m.Shutdown(context.Background()))
}
