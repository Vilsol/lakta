package lakta_test

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
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

// Marker types used only for dependency graph testing.
type (
	depTypeA struct{}
	depTypeB struct{}
	depTypeC struct{}
)

func TestRuntime_InitSequential(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	var seq1, seq2, seq3 int64

	m1 := testkit.NewMockModule()
	m2 := testkit.NewMockModule()
	m3 := testkit.NewMockModule()

	m1.OnInit = func(_ context.Context) error { seq1 = counter.Add(1); return nil }
	m2.OnInit = func(_ context.Context) error { seq2 = counter.Add(1); return nil }
	m3.OnInit = func(_ context.Context) error { seq3 = counter.Add(1); return nil }

	rh := testkit.NewRuntimeHarness(t, m1, m2, m3)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int64(1), seq1)
	testza.AssertEqual(t, int64(2), seq2)
	testza.AssertEqual(t, int64(3), seq3)
}

func TestRuntime_InitErrorAbortsRemaining(t *testing.T) {
	t.Parallel()

	m1 := testkit.NewMockModule()
	m1.InitErr = errors.New("init failed")
	m2 := testkit.NewMockModule()
	m3 := testkit.NewMockModule()

	rh := testkit.NewRuntimeHarness(t, m1, m2, m3)
	_ = rh.Shutdown()

	testza.AssertEqual(t, int32(0), m2.InitCalls.Load())
	testza.AssertEqual(t, int32(0), m3.InitCalls.Load())
}

func TestRuntime_InitErrorReturnsError(t *testing.T) {
	t.Parallel()

	initErr := errors.New("boom")
	m1 := testkit.NewMockModule()
	m1.InitErr = initErr

	rh := testkit.NewRuntimeHarness(t, m1)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), initErr.Error())
}

func TestRuntime_AsyncOnlyBlocksUntilShutdown(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockAsyncModule()

	shutdownStarted := make(chan struct{})
	m.OnShutdown = func(_ context.Context) error {
		close(shutdownStarted)
		return nil
	}

	rh := testkit.NewRuntimeHarness(t, m)

	// Give async start time to complete. With the old code the runtime would have
	// already proceeded to shutdown; with the fix it must still be running.
	time.Sleep(10 * time.Millisecond)

	select {
	case <-shutdownStarted:
		t.Fatal("runtime proceeded to shutdown before receiving shutdown signal")
	default:
	}

	testza.AssertNil(t, rh.Shutdown())
}

func TestRuntime_AsyncModuleStarted(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockAsyncModule()
	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int32(1), m.StartAsyncCalls.Load())
}

func TestRuntime_SyncModuleStarted(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockSyncModule()
	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int32(1), m.StartCalls.Load())
}

func TestRuntime_PlainModuleNotStarted(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockModule()
	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int32(1), m.InitCalls.Load())
	testza.AssertEqual(t, int32(1), m.ShutdownCalls.Load())
}

func TestRuntime_GracefulShutdown(t *testing.T) {
	t.Parallel()

	m1 := testkit.NewMockModule()
	m2 := testkit.NewMockModule()

	rh := testkit.NewRuntimeHarness(t, m1, m2)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int32(1), m1.InitCalls.Load())
	testza.AssertEqual(t, int32(1), m1.ShutdownCalls.Load())
	testza.AssertEqual(t, int32(1), m2.InitCalls.Load())
	testza.AssertEqual(t, int32(1), m2.ShutdownCalls.Load())
}

func TestRuntime_StartErrorCausesShutdown(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	m := testkit.NewMockSyncModule()
	m.StartErr = startErr

	rh := testkit.NewRuntimeHarness(t, m)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), startErr.Error())
	testza.AssertEqual(t, int32(1), m.StartCalls.Load())
}

func TestRuntime_ShutdownErrorsReturned(t *testing.T) {
	t.Parallel()

	shutdownErr := errors.New("shutdown failed")
	m := testkit.NewMockModule()
	m.ShutdownErr = shutdownErr

	rh := testkit.NewRuntimeHarness(t, m)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
}

func TestRuntime_ConcurrentStart(t *testing.T) {
	t.Parallel()

	m1 := testkit.NewMockSyncModule()
	m2 := testkit.NewMockSyncModule()

	rh := testkit.NewRuntimeHarness(t, m1, m2)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int32(1), m1.StartCalls.Load())
	testza.AssertEqual(t, int32(1), m2.StartCalls.Load())
}

func TestRuntime_DefaultLoggerFallback(t *testing.T) {
	t.Parallel()

	// No *slog.Logger in DI — runtime should fall back to slog.Default() and succeed.
	m := testkit.NewMockModule()
	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())
}

func TestRuntime_AutoSort(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	var seqA, seqB, seqC int64

	typeA := reflect.TypeFor[*depTypeA]()
	typeB := reflect.TypeFor[*depTypeB]()
	typeC := reflect.TypeFor[*depTypeC]()

	mA := testkit.NewMockProviderModule()
	mA.ProvidesTypes = []reflect.Type{typeA}
	mA.OnInit = func(_ context.Context) error { seqA = counter.Add(1); return nil }

	mB := testkit.NewMockProviderModule()
	mB.ProvidesTypes = []reflect.Type{typeB}
	mB.OptionalDeps = []reflect.Type{typeA}
	mB.OnInit = func(_ context.Context) error { seqB = counter.Add(1); return nil }

	mC := testkit.NewMockProviderModule()
	mC.ProvidesTypes = []reflect.Type{typeC}
	mC.RequiredDeps = []reflect.Type{typeB}
	mC.OnInit = func(_ context.Context) error { seqC = counter.Add(1); return nil }

	// Intentionally pass in reverse dependency order.
	rh := testkit.NewRuntimeHarness(t, mC, mB, mA)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertTrue(t, seqA < seqB)
	testza.AssertTrue(t, seqB < seqC)
}

func TestRuntime_CycleDetected(t *testing.T) {
	t.Parallel()

	typeA := reflect.TypeFor[*depTypeA]()
	typeB := reflect.TypeFor[*depTypeB]()

	mA := testkit.NewMockProviderModule()
	mA.ProvidesTypes = []reflect.Type{typeA}
	mA.RequiredDeps = []reflect.Type{typeB}

	mB := testkit.NewMockProviderModule()
	mB.ProvidesTypes = []reflect.Type{typeB}
	mB.RequiredDeps = []reflect.Type{typeA}

	rh := testkit.NewRuntimeHarness(t, mA, mB)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, int32(0), mA.InitCalls.Load())
	testza.AssertEqual(t, int32(0), mB.InitCalls.Load())
}

func TestRuntime_MissingRequiredDep(t *testing.T) {
	t.Parallel()

	typeA := reflect.TypeFor[*depTypeA]()

	m := testkit.NewMockProviderModule()
	m.RequiredDeps = []reflect.Type{typeA} // nothing provides typeA

	rh := testkit.NewRuntimeHarness(t, m)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, int32(0), m.InitCalls.Load())
}

func TestRuntime_OptionalDepAbsent(t *testing.T) {
	t.Parallel()

	typeA := reflect.TypeFor[*depTypeA]()

	m := testkit.NewMockProviderModule()
	m.OptionalDeps = []reflect.Type{typeA} // nothing provides typeA — should be fine

	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, int32(1), m.InitCalls.Load())
}

func TestRuntime_OptionalDepPresent_Ordered(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	var seqProvider, seqConsumer int64

	typeA := reflect.TypeFor[*depTypeA]()

	provider := testkit.NewMockProviderModule()
	provider.ProvidesTypes = []reflect.Type{typeA}
	provider.OnInit = func(_ context.Context) error { seqProvider = counter.Add(1); return nil }

	consumer := testkit.NewMockProviderModule()
	consumer.OptionalDeps = []reflect.Type{typeA}
	consumer.OnInit = func(_ context.Context) error { seqConsumer = counter.Add(1); return nil }

	// Pass consumer before provider — sort should fix it.
	rh := testkit.NewRuntimeHarness(t, consumer, provider)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertTrue(t, seqProvider < seqConsumer)
}

func TestRuntime_ShutdownOnInitFailure(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	var shutSeq1, shutSeq2 int64

	m1 := testkit.NewMockModule()
	m1.OnShutdown = func(_ context.Context) error { shutSeq1 = counter.Add(1); return nil }

	m2 := testkit.NewMockModule()
	m2.OnShutdown = func(_ context.Context) error { shutSeq2 = counter.Add(1); return nil }

	m3 := testkit.NewMockModule()
	m3.InitErr = errors.New("init failed")

	rh := testkit.NewRuntimeHarness(t, m1, m2, m3)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, int32(0), m3.ShutdownCalls.Load())
	// m2 inited before m3 failed, so m2 shuts down before m1
	testza.AssertTrue(t, shutSeq2 < shutSeq1)
}

func TestRuntime_ShutdownOrder(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	var shutSeq1, shutSeq2, shutSeq3 int64

	m1 := testkit.NewMockModule()
	m1.OnShutdown = func(_ context.Context) error { shutSeq1 = counter.Add(1); return nil }

	m2 := testkit.NewMockModule()
	m2.OnShutdown = func(_ context.Context) error { shutSeq2 = counter.Add(1); return nil }

	m3 := testkit.NewMockModule()
	m3.OnShutdown = func(_ context.Context) error { shutSeq3 = counter.Add(1); return nil }

	rh := testkit.NewRuntimeHarness(t, m1, m2, m3)
	testza.AssertNil(t, rh.Shutdown())

	// Shutdown must be reverse of init order: m3, m2, m1
	testza.AssertTrue(t, shutSeq3 < shutSeq2)
	testza.AssertTrue(t, shutSeq2 < shutSeq1)
}

type configurableMock struct {
	counter         *atomic.Int64
	loadConfigOrder int64
	initOrder       int64
	loadConfigError error
	initCalled      bool
	gotKoanf        *koanf.Koanf
}

func (m *configurableMock) Init(_ context.Context) error {
	m.initCalled = true
	m.initOrder = m.counter.Add(1)
	return nil
}

func (m *configurableMock) Shutdown(_ context.Context) error { return nil }

func (m *configurableMock) ConfigPath() string { return "test" }

func (m *configurableMock) LoadConfig(k *koanf.Koanf) error {
	if m.loadConfigError != nil {
		return m.loadConfigError
	}
	m.loadConfigOrder = m.counter.Add(1)
	m.gotKoanf = k
	return nil
}

func koanfProviderModule(k *koanf.Koanf) *testkit.MockModule {
	m := testkit.NewMockModule()
	m.OnInit = func(ctx context.Context) error {
		lakta.Provide(ctx, func(_ do.Injector) (*koanf.Koanf, error) {
			return k, nil
		})
		return nil
	}
	return m
}

func TestRuntime_AutoLoadsConfigBeforeInit(t *testing.T) {
	t.Parallel()

	var counter atomic.Int64
	k := koanf.New(".")
	_ = k.Set("test", "value") // make k.Exists("test") true

	configurable := &configurableMock{counter: &counter}

	rh := testkit.NewRuntimeHarness(t, koanfProviderModule(k), configurable)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertEqual(t, k, configurable.gotKoanf)
	testza.AssertTrue(t, configurable.loadConfigOrder < configurable.initOrder)
}

func TestRuntime_AutoLoadConfigError_AbortsInit(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("bad config")
	configurable := &configurableMock{
		counter:         &atomic.Int64{},
		loadConfigError: loadErr,
	}

	k := koanf.New(".")
	_ = k.Set("test", "value") // make k.Exists("test") true so LoadConfig is called
	rh := testkit.NewRuntimeHarness(t, koanfProviderModule(k), configurable)
	err := rh.Shutdown()

	testza.AssertNotNil(t, err)
	testza.AssertFalse(t, configurable.initCalled)
}
