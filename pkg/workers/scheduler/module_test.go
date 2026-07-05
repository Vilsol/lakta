package scheduler_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/Vilsol/lakta/pkg/workers/scheduler"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

const (
	prefix    = "modules.workers.scheduler.default.jobs."
	everyHour = "@every 1h"
)

func setup(t *testing.T, data map[string]any, options ...scheduler.Option) (*scheduler.Scheduler, *scheduler.Module, *logSpy) {
	t.Helper()
	h := testkit.NewHarness(t)
	spy := newLogSpy()
	ctx := slox.Into(h.Ctx(), slog.New(spy))

	m := scheduler.NewModule(options...)

	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(data, "."), nil))
	testza.AssertNoError(t, m.LoadConfig(k))
	testza.AssertNoError(t, m.Init(ctx))
	testza.AssertNoError(t, m.StartAsync(ctx))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	sched, err := do.Invoke[*scheduler.Scheduler](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)

	return sched, m, spy
}

func TestModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.workers.scheduler.default", scheduler.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.workers.scheduler.custom", scheduler.NewModule(scheduler.WithName("custom")).ConfigPath())
}

func TestScheduler_RunNowFiresAndUnknownErrors(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	sched, _, _ := setup(t, map[string]any{},
		scheduler.WithJob("hello", everyHour, func(context.Context) error {
			close(done)
			return nil
		}))

	testza.AssertNoError(t, sched.RunNow("hello"))
	waitFor(t, done)

	err := sched.RunNow("nope")
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "nope")
	testza.AssertContains(t, err.Error(), "hello") // lists known names
}

func TestScheduler_OverlapAllowRunsConcurrently(t *testing.T) {
	t.Parallel()
	b := &blocker{gate: make(chan struct{})}
	sched, _, _ := setup(t, map[string]any{prefix + "j.overlap": "allow"},
		scheduler.WithJob("j", everyHour, b.handle))

	testza.AssertNoError(t, sched.RunNow("j"))
	waitUntil(t, func() bool { return b.active.Load() >= 1 }, "first run active")
	testza.AssertNoError(t, sched.RunNow("j"))
	waitUntil(t, func() bool { return b.maxActive.Load() == 2 }, "both runs concurrent")

	testza.AssertEqual(t, int64(2), b.maxActive.Load())
}

func TestScheduler_OverlapSkipDropsOverlappingRun(t *testing.T) {
	t.Parallel()
	b := &blocker{gate: make(chan struct{})}
	sched, _, _ := setup(t, map[string]any{prefix + "j.overlap": "skip"},
		scheduler.WithJob("j", everyHour, b.handle))

	testza.AssertNoError(t, sched.RunNow("j"))
	waitUntil(t, func() bool { return b.active.Load() >= 1 }, "first run active")
	testza.AssertNoError(t, sched.RunNow("j")) // dropped: singleton reschedule
	time.Sleep(100 * time.Millisecond)

	testza.AssertEqual(t, int64(1), b.maxActive.Load())
	b.gate <- struct{}{} // release the only run
	time.Sleep(50 * time.Millisecond)
	testza.AssertEqual(t, int64(1), b.total.Load())
}

func TestScheduler_OverlapQueueSerializesRuns(t *testing.T) {
	t.Parallel()
	b := &blocker{gate: make(chan struct{})}
	sched, _, _ := setup(t, map[string]any{prefix + "j.overlap": "queue"},
		scheduler.WithJob("j", everyHour, b.handle))

	testza.AssertNoError(t, sched.RunNow("j"))
	waitUntil(t, func() bool { return b.active.Load() >= 1 }, "first run active")
	testza.AssertNoError(t, sched.RunNow("j")) // queued: waits for the first
	time.Sleep(100 * time.Millisecond)

	testza.AssertEqual(t, int64(1), b.maxActive.Load()) // never concurrent
	b.gate <- struct{}{}                                // release first; queued run starts
	waitUntil(t, func() bool { return b.total.Load() == 2 }, "queued run executes")
}

func TestScheduler_PerJobTimezone(t *testing.T) {
	t.Parallel()
	sched, _, _ := setup(t, map[string]any{prefix + "noon.timezone": "America/New_York"},
		scheduler.WithJob("noon", "0 0 12 * * *", noop))

	ny, err := time.LoadLocation("America/New_York")
	testza.AssertNoError(t, err)

	next, err := sched.NextRun("noon")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 12, next.In(ny).Hour()) // fires at noon NY, not noon UTC
	testza.AssertEqual(t, 0, next.In(ny).Minute())
}

func TestModule_DisabledJobNeverRegistered(t *testing.T) {
	t.Parallel()
	sched, _, _ := setup(t, map[string]any{prefix + "cleanup.enabled": false},
		scheduler.WithJob("cleanup", everyHour, noop))

	err := sched.RunNow("cleanup")
	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, 0, len(sched.Jobs()))
}

func TestModule_HotReloadRewiresLiveJobs(t *testing.T) {
	t.Parallel()
	keepRan := make(chan struct{}, 1)
	sched, m, _ := setup(t, map[string]any{prefix + "addme.enabled": false},
		scheduler.WithJob("keep", everyHour, func(context.Context) error {
			select {
			case keepRan <- struct{}{}:
			default:
			}
			return nil
		}),
		scheduler.WithJob("changeme", everyHour, noop),
		scheduler.WithJob("removeme", everyHour, noop),
		scheduler.WithJob("addme", everyHour, noop),
	)

	testza.AssertEqual(t, []string{"changeme", "keep", "removeme"}, jobNames(sched.Jobs()))
	before, err := sched.NextRun("changeme")
	testza.AssertNoError(t, err)

	rk := koanf.New(".")
	testza.AssertNoError(t, rk.Load(confmap.Provider(map[string]any{
		prefix + "changeme.schedule": "@every 3h",
		prefix + "removeme.enabled":  false,
		prefix + "addme.enabled":     true,
	}, "."), nil))
	m.OnReload(rk)

	testza.AssertEqual(t, []string{"addme", "changeme", "keep"}, jobNames(sched.Jobs()))

	after, err := sched.NextRun("changeme")
	testza.AssertNoError(t, err)
	testza.AssertTrue(t, after.Sub(before) > 30*time.Minute) // rescheduled 1h -> 3h

	testza.AssertNoError(t, sched.RunNow("keep")) // survivor handler still fires
	waitFor(t, keepRan)
}

func TestModule_ShutdownDrainsInflight(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	sched, m, _ := setup(t, map[string]any{},
		scheduler.WithJob("slow", everyHour, func(context.Context) error {
			started <- struct{}{}
			<-gate // deliberately ignores ctx: must finish naturally
			return nil
		}))

	testza.AssertNoError(t, sched.RunNow("slow"))
	waitFor(t, started)

	done := make(chan error, 1)
	go func() { done <- m.Shutdown(context.Background()) }()

	select {
	case <-done:
		t.Fatal("Shutdown returned before in-flight job completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(gate)
	testza.AssertNoError(t, waitFor(t, done))
}

func TestModule_ShutdownTimesOutWhenJobStuck(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()

	started := make(chan struct{}, 1)
	sched, m, _ := setup(t, map[string]any{},
		scheduler.WithJob("stuck", everyHour, func(context.Context) error {
			started <- struct{}{}
			<-gate
			return nil
		}))

	testza.AssertNoError(t, sched.RunNow("stuck"))
	waitFor(t, started)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := m.Shutdown(ctx)
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "timed out")
}

func TestScheduler_PanicRecoveryIsolatesFailure(t *testing.T) {
	t.Parallel()
	okRan := make(chan struct{}, 1)
	sched, _, spy := setup(t, map[string]any{},
		scheduler.WithJob("boom", everyHour, func(context.Context) error { panic("kaboom") }),
		scheduler.WithJob("ok", everyHour, func(context.Context) error {
			okRan <- struct{}{}
			return nil
		}),
	)

	testza.AssertNoError(t, sched.RunNow("boom"))
	waitUntil(t, func() bool { return spy.contains("panicked") }, "panic logged")

	testza.AssertNoError(t, sched.RunNow("ok")) // scheduler survives the panic
	waitFor(t, okRan)
	testza.AssertTrue(t, spy.contains("panicked"))
}

// --- helpers ---

func noop(context.Context) error { return nil }

// blocker records concurrency for overlap-policy assertions; its handler blocks
// on gate until released (and on ctx cancellation so shutdown never hangs).
type blocker struct {
	active    atomic.Int64
	maxActive atomic.Int64
	total     atomic.Int64
	gate      chan struct{}
}

func (b *blocker) handle(ctx context.Context) error {
	n := b.active.Add(1)
	for {
		o := b.maxActive.Load()
		if n <= o || b.maxActive.CompareAndSwap(o, n) {
			break
		}
	}
	b.total.Add(1)

	select {
	case <-b.gate:
	case <-ctx.Done():
	}

	b.active.Add(-1)

	return nil
}

func waitFor[T any](t *testing.T, ch <-chan T) T { //nolint:ireturn // generic helper returns the received value
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel")
		var zero T
		return zero
	}
}

func waitUntil(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func jobNames(infos []scheduler.JobInfo) []string {
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name
	}
	return names
}

type logSpy struct {
	mu   sync.Mutex
	msgs []string
}

func newLogSpy() *logSpy { return &logSpy{} }

func (s *logSpy) Enabled(context.Context, slog.Level) bool { return true }

func (s *logSpy) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, r.Message)
	return nil
}

func (s *logSpy) WithAttrs([]slog.Attr) slog.Handler { return s }

func (s *logSpy) WithGroup(string) slog.Handler { return s }

func (s *logSpy) contains(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}
