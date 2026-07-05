package lakta

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/samber/do/v2"
)

type blockingShutdownModule struct {
	release chan struct{}
}

func (blockingShutdownModule) Init(context.Context) error       { return nil }
func (m blockingShutdownModule) Shutdown(context.Context) error { <-m.release; return nil }

type countingShutdownModule struct{ calls atomic.Int32 }

func (*countingShutdownModule) Init(context.Context) error { return nil }
func (m *countingShutdownModule) Shutdown(context.Context) error {
	m.calls.Add(1)
	return nil
}

type quickModule struct{ err error }

func (quickModule) Init(context.Context) error       { return nil }
func (m quickModule) Shutdown(context.Context) error { return m.err }

type panicModule struct{}

func (panicModule) Init(context.Context) error     { return nil }
func (panicModule) Shutdown(context.Context) error { panic("shutdown boom") }

func TestShutdownModule_ReturnsOnDeadline(t *testing.T) {
	t.Parallel()

	m := blockingShutdownModule{release: make(chan struct{})}
	t.Cleanup(func() { close(m.release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := shutdownModule(ctx, m)

	testza.AssertNotNil(t, err)
	testza.AssertTrue(t, time.Since(start) < time.Second, "shutdownModule must return near the deadline, not block on Shutdown")
}

func TestShutdownModule_ReturnsResultWhenFast(t *testing.T) {
	t.Parallel()

	testza.AssertNil(t, shutdownModule(context.Background(), quickModule{}))

	wantErr := errors.New("x")
	testza.AssertNotNil(t, shutdownModule(context.Background(), quickModule{err: wantErr}))
}

func TestShutdownModule_RecoversPanic(t *testing.T) {
	t.Parallel()

	err := shutdownModule(context.Background(), panicModule{})

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "shutdown boom")
}

func TestSafeCall_RecoversPanic(t *testing.T) {
	t.Parallel()

	err := safeCall(func() error { panic("boom") })

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "boom")
}

func TestShutdown_DeadlineExceeded_SkipsRemaining(t *testing.T) {
	t.Parallel()

	skipped := &countingShutdownModule{}
	blocker := blockingShutdownModule{release: make(chan struct{})}
	t.Cleanup(func() { close(blocker.release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := &Runtime{}
	// Reverse order: blocker shuts down first and runs past the deadline, so the
	// remaining module is skipped and a deadline error is returned.
	err := r.shutdown(ctx, []Module{skipped, blocker}, nil)

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "deadline exceeded")
	testza.AssertEqual(t, int32(0), skipped.calls.Load())
}

type (
	depA struct{}
	depB struct{}
)

type diType struct{ val string }

// declModule declares configurable Provides/Dependencies for graph tests.
type declModule struct {
	initCalls atomic.Int32
	provides  []reflect.Type
	required  []reflect.Type
	optional  []reflect.Type
}

func (m *declModule) Init(context.Context) error { m.initCalls.Add(1); return nil }
func (*declModule) Shutdown(context.Context) error {
	return nil
}
func (m *declModule) Provides() []reflect.Type { return m.provides }
func (m *declModule) Dependencies() ([]reflect.Type, []reflect.Type) {
	return m.required, m.optional
}

// infoProviderModule provides *diType in DI during Init.
type infoProviderModule struct {
	NamedBase

	initCalls atomic.Int32
}

func (m *infoProviderModule) Init(ctx context.Context) error {
	m.initCalls.Add(1)
	ProvideValue(ctx, &diType{val: "provided"})
	return nil
}
func (*infoProviderModule) Shutdown(context.Context) error { return nil }
func (*infoProviderModule) Provides() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*diType]()}
}

// infoSyncModule requires *diType and blocks in Start until ctx is done.
type infoSyncModule struct {
	started chan struct{}
	once    sync.Once
}

func (*infoSyncModule) Init(context.Context) error     { return nil }
func (*infoSyncModule) Shutdown(context.Context) error { return nil }
func (m *infoSyncModule) Start(ctx context.Context) error {
	m.once.Do(func() {
		if m.started != nil {
			close(m.started)
		}
	})
	<-ctx.Done()
	return nil
}

func (*infoSyncModule) Dependencies() ([]reflect.Type, []reflect.Type) {
	return []reflect.Type{reflect.TypeFor[*diType]()}, nil
}

// infoAsyncModule is a minimal AsyncModule for lifecycle classification.
type infoAsyncModule struct{}

func (*infoAsyncModule) Init(context.Context) error       { return nil }
func (*infoAsyncModule) Shutdown(context.Context) error   { return nil }
func (*infoAsyncModule) StartAsync(context.Context) error { return nil }

const testWaitTimeout = 2 * time.Second

func waitForState(t *testing.T, info *RuntimeInfo, order int, want ModuleState) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) {
		if info.Snapshot()[order].State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("module %d never reached state %s", order, want)
}

func TestRunContext_PopulatesRuntimeInfo(t *testing.T) {
	t.Parallel()

	provider := &infoProviderModule{NamedBase: NewNamedBase("prov")}
	syncMod := &infoSyncModule{started: make(chan struct{})}

	injector := do.New()
	ctx, cancel := context.WithCancel(WithInjector(context.Background(), injector))
	defer cancel()

	// Listed sync-first on purpose: topo sort must put the provider first.
	r := NewRuntime(syncMod, provider)
	done := make(chan error, 1)
	go func() { done <- r.RunContext(ctx) }()

	select {
	case <-syncMod.started:
	case <-time.After(testWaitTimeout):
		t.Fatal("sync module never started")
	}

	info, err := do.Invoke[*RuntimeInfo](injector)
	testza.AssertNil(t, err)

	snap := info.Snapshot()
	testza.AssertEqual(t, 2, len(snap))

	testza.AssertEqual(t, "prov", snap[0].Name)
	testza.AssertEqual(t, "*lakta.infoProviderModule", snap[0].Type)
	testza.AssertEqual(t, 0, snap[0].InitOrder)
	testza.AssertEqual(t, []string{"*lakta.diType"}, snap[0].Provides)
	testza.AssertEqual(t, LifecycleInit, snap[0].Lifecycle)
	testza.AssertEqual(t, StateInitialized, snap[0].State)
	testza.AssertTrue(t, snap[0].InitDuration > 0, "InitDuration must be captured")

	testza.AssertEqual(t, "", snap[1].Name)
	testza.AssertEqual(t, 1, snap[1].InitOrder)
	testza.AssertEqual(t, []string{"*lakta.diType"}, snap[1].Requires)
	testza.AssertEqual(t, LifecycleSync, snap[1].Lifecycle)

	waitForState(t, info, 1, StateStarted)

	cancel()
	testza.AssertNil(t, <-done)

	snap = info.Snapshot()
	testza.AssertEqual(t, StateStopped, snap[0].State)
	testza.AssertEqual(t, StateStopped, snap[1].State)
}

func TestValidate_ValidGraph(t *testing.T) {
	t.Parallel()

	provider := &infoProviderModule{}
	syncMod := &infoSyncModule{}

	r := NewRuntime(syncMod, provider)
	testza.AssertNil(t, r.Validate())
	testza.AssertEqual(t, int32(0), provider.initCalls.Load(), "Validate must not run Init")
}

func TestValidate_Cycle(t *testing.T) {
	t.Parallel()

	m1 := &declModule{
		provides: []reflect.Type{reflect.TypeFor[*depA]()},
		required: []reflect.Type{reflect.TypeFor[*depB]()},
	}
	m2 := &declModule{
		provides: []reflect.Type{reflect.TypeFor[*depB]()},
		required: []reflect.Type{reflect.TypeFor[*depA]()},
	}

	err := NewRuntime(m1, m2).Validate()

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "cycle")
	testza.AssertEqual(t, int32(0), m1.initCalls.Load())
	testza.AssertEqual(t, int32(0), m2.initCalls.Load())
}

func TestValidate_UnmetDependency(t *testing.T) {
	t.Parallel()

	m := &declModule{required: []reflect.Type{reflect.TypeFor[*depA]()}}

	err := NewRuntime(m).Validate()

	testza.AssertNotNil(t, err)
	testza.AssertTrue(t, errors.Is(err, ErrUnmetDependency), "unmet-dep errors must match errors.Is(err, ErrUnmetDependency)")
	testza.AssertContains(t, err.Error(), "no module provides it")
	testza.AssertEqual(t, int32(0), m.initCalls.Load(), "Validate must not run Init")
}

// captureModule resolves *diType from ctx during Init, proving which injector RunContext used.
type captureModule struct {
	got     *diType
	initErr error
	inited  chan struct{}
}

func (m *captureModule) Init(ctx context.Context) error {
	m.got, m.initErr = Invoke[*diType](ctx)
	close(m.inited)
	return nil
}
func (*captureModule) Shutdown(context.Context) error { return nil }

func TestRunContext_AdoptsCtxInjector(t *testing.T) {
	t.Parallel()

	injector := do.New()
	do.ProvideValue(injector, &diType{val: "seeded"})

	ctx, cancel := context.WithCancel(WithInjector(context.Background(), injector))
	defer cancel()

	m := &captureModule{inited: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- NewRuntime(m).RunContext(ctx) }()

	select {
	case <-m.inited:
	case <-time.After(testWaitTimeout):
		t.Fatal("module never initialized")
	}

	cancel()
	testza.AssertNil(t, <-done)

	testza.AssertNil(t, m.initErr)
	testza.AssertNotNil(t, m.got)
	testza.AssertEqual(t, "seeded", m.got.val)
}

func TestRunContext_NoInjectorPathUnchanged(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &captureModule{inited: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- NewRuntime(m).RunContext(ctx) }()

	select {
	case <-m.inited:
	case <-time.After(testWaitTimeout):
		t.Fatal("module never initialized")
	}

	cancel()
	testza.AssertNil(t, <-done)

	// A fresh do.New injector was created: Invoke inside Init did not panic,
	// and the (unseeded) lookup simply errored.
	testza.AssertNotNil(t, m.initErr)
	testza.AssertNil(t, m.got)
}
