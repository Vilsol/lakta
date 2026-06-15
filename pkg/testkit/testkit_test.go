package testkit_test

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

func TestHarness_CtxCarriesInjector(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	testza.AssertNotNil(t, h.Injector())
	testza.AssertNotNil(t, lakta.GetInjector(h.Ctx()))
}

func TestHarness_WithData_ProvidesKoanfAndNotifier(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{"greeting": "hello"})

	k, err := do.Invoke[*koanf.Koanf](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "hello", k.String("greeting"))

	n, err := do.Invoke[config.ReloadNotifier](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, n)
}

func TestHarness_WithKoanf_NotifierIsShared(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithKoanf(koanf.New("."))

	n, err := do.Invoke[config.ReloadNotifier](h.Injector())
	testza.AssertNil(t, err)

	// The notifier provided in DI must be the same instance Notifier() exposes.
	nt, ok := n.(*testkit.ReloadNotifier)
	testza.AssertTrue(t, ok)
	testza.AssertTrue(t, nt == h.Notifier())
}

func TestHarness_WithProvider(t *testing.T) {
	t.Parallel()

	type custom struct{ v int }

	h := testkit.NewHarness(t)
	testkit.WithProvider(h, func(_ do.Injector) (*custom, error) {
		return &custom{v: 42}, nil
	})

	got, err := do.Invoke[*custom](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 42, got.v)
}

func TestMapProvider(t *testing.T) {
	t.Parallel()

	p := testkit.MapProvider{"x": 1}

	m, err := p.Read()
	testza.AssertNil(t, err)
	testza.AssertEqual(t, 1, m["x"])

	_, err = p.ReadBytes()
	testza.AssertNotNil(t, err)
}

func TestReloadNotifier_FireReloadInvokesCallbacks(t *testing.T) {
	t.Parallel()

	n := &testkit.ReloadNotifier{}

	var got *koanf.Koanf
	calls := 0
	n.OnReload(func(k *koanf.Koanf) {
		got = k
		calls++
	})
	n.OnValidate(func(*koanf.Koanf) error { return nil }) // no-op double, must not panic

	k := koanf.New(".")
	n.FireReload(k)

	testza.AssertEqual(t, 1, calls)
	testza.AssertEqual(t, k, got)
}

func TestMockModule_CountsAndErrors(t *testing.T) {
	t.Parallel()

	initErr := errors.New("init boom")
	m := testkit.NewMockModule()
	m.InitErr = initErr

	testza.AssertEqual(t, initErr, m.Init(context.Background()))
	testza.AssertEqual(t, int32(1), m.InitCalls.Load())

	shutdownCalled := false
	m.OnShutdown = func(context.Context) error {
		shutdownCalled = true
		return nil
	}
	testza.AssertNil(t, m.Shutdown(context.Background()))
	testza.AssertTrue(t, shutdownCalled)
	testza.AssertEqual(t, int32(1), m.ShutdownCalls.Load())
}

func TestMockSyncModule_BlockStartReleasedByCtx(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockSyncModule()
	m.BlockStart = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	testza.AssertNotNil(t, m.Start(ctx)) // cancelled ctx releases the block as an error
	testza.AssertEqual(t, int32(1), m.StartCalls.Load())
}

func TestMockSyncModule_BlockStartReleasedByClose(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockSyncModule()
	m.BlockStart = make(chan struct{})
	close(m.BlockStart)

	testza.AssertNil(t, m.Start(context.Background()))
	testza.AssertEqual(t, int32(1), m.StartCalls.Load())
}

func TestMockAsyncModule_CountsAndError(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockAsyncModule()
	m.StartAsyncErr = errors.New("async boom")

	testza.AssertNotNil(t, m.StartAsync(context.Background()))
	testza.AssertEqual(t, int32(1), m.StartAsyncCalls.Load())
}

func TestMockProviderModule_Declarations(t *testing.T) {
	t.Parallel()

	typeA := reflect.TypeFor[*struct{}]()

	m := testkit.NewMockProviderModule()
	m.ProvidesTypes = []reflect.Type{typeA}
	m.RequiredDeps = []reflect.Type{typeA}

	testza.AssertEqual(t, []reflect.Type{typeA}, m.Provides())

	req, opt := m.Dependencies()
	testza.AssertEqual(t, []reflect.Type{typeA}, req)
	testza.AssertNil(t, opt)
}

func TestRuntimeHarness_ShutdownIdempotentAndErr(t *testing.T) {
	t.Parallel()

	m := testkit.NewMockModule()
	rh := testkit.NewRuntimeHarness(t, m)

	err1 := rh.Shutdown()
	err2 := rh.Shutdown() // second call must be a no-op returning the same result

	testza.AssertNil(t, err1)
	testza.AssertEqual(t, err1, err2)
	testza.AssertEqual(t, err1, rh.Err())
	testza.AssertEqual(t, int32(1), m.InitCalls.Load())
	testza.AssertEqual(t, int32(1), m.ShutdownCalls.Load())
}

type fakeAddrProvider struct{ addr net.Addr }

func (f fakeAddrProvider) Addr() net.Addr { return f.addr }

func TestWaitForAddr_ReturnsAddr(t *testing.T) {
	t.Parallel()

	want := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	got := testkit.WaitForAddr(t, fakeAddrProvider{addr: want})
	testza.AssertEqual(t, want, got)
}
