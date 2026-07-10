package testkit_test

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
)

const startTimeout = 2 * time.Second

// fakeRegistry is a mocked collaborator injected via Mock[*fakeRegistry].
type fakeRegistry struct{ name string }

// dependentModule declares *fakeRegistry as an optional dependency (so it boots
// when the collaborator is mocked rather than included as a module), invokes it
// during Init to prove the mock survived, and self-registers so Get[*dependentModule]
// retrieves the booted module.
type dependentModule struct {
	initCalls atomic.Int32
	gotReg    *fakeRegistry
	inited    chan struct{}
}

func (d *dependentModule) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{reflect.TypeFor[*fakeRegistry]()}
}

func (d *dependentModule) Init(ctx context.Context) error {
	d.initCalls.Add(1)
	reg, err := lakta.Invoke[*fakeRegistry](ctx)
	if err != nil {
		return err
	}
	d.gotReg = reg
	lakta.ProvideValue(ctx, d)
	close(d.inited)
	return nil
}

func (d *dependentModule) Shutdown(_ context.Context) error { return nil }

// loggingModule logs through the DI logger during Init, so WithTestLogger bridges
// the record to t.Log.
type loggingModule struct {
	initCalls atomic.Int32
	inited    chan struct{}
}

func (m *loggingModule) Init(ctx context.Context) error {
	m.initCalls.Add(1)
	if logger, err := lakta.Invoke[*slog.Logger](ctx); err == nil {
		logger.Info("logging module init")
	}
	close(m.inited)
	return nil
}

func (m *loggingModule) Shutdown(_ context.Context) error { return nil }

// reloadableModule is HotReloadable and signals via started once the runtime has
// wired its OnReload callback (StartAsync runs after that wiring).
type reloadableModule struct {
	reloaded atomic.Int32
	started  chan struct{}
}

func (m *reloadableModule) Init(_ context.Context) error       { return nil }
func (m *reloadableModule) Shutdown(_ context.Context) error   { return nil }
func (m *reloadableModule) OnReload(_ *koanf.Koanf)            { m.reloaded.Add(1) }
func (m *reloadableModule) StartAsync(_ context.Context) error { close(m.started); return nil }

// undeclaredModule invokes a dependency it never declared; Validate cannot see it,
// so the failure only surfaces at Init (the documented escape hatch).
type undeclaredModule struct{ initCalls atomic.Int32 }

func (m *undeclaredModule) Init(ctx context.Context) error {
	m.initCalls.Add(1)
	_, err := lakta.Invoke[*fakeRegistry](ctx)
	return err
}

func (m *undeclaredModule) Shutdown(_ context.Context) error { return nil }

func containsSubstr(haystack []string, sub string) bool {
	for _, h := range haystack {
		if strings.Contains(h, sub) {
			return true
		}
	}
	return false
}

func waitInit(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(startTimeout):
		t.Fatal("module did not initialize in time")
	}
}

func TestSlice_MockResolution(t *testing.T) {
	t.Parallel()

	dep := &dependentModule{inited: make(chan struct{})}
	reg := &fakeRegistry{name: "mocked"}

	s := testkit.NewSlice(t, dep)
	testkit.Mock[*fakeRegistry](s, reg)
	s.Start()
	waitInit(t, dep.inited)

	got := testkit.Get[*fakeRegistry](s)
	testza.AssertEqual(t, reg, got)
	testza.AssertEqual(t, int32(1), dep.initCalls.Load())
	testza.AssertEqual(t, "mocked", dep.gotReg.name)
}

func TestSlice_FullBoot(t *testing.T) {
	t.Parallel()

	dep := &dependentModule{inited: make(chan struct{})}
	reg := &fakeRegistry{name: "reg"}

	s := testkit.NewSlice(t, dep)
	testkit.Mock[*fakeRegistry](s, reg)
	s.Start()
	waitInit(t, dep.inited)

	got := testkit.Get[*dependentModule](s)
	testza.AssertEqual(t, dep, got)

	provided := s.Provided()
	testza.AssertTrue(t, containsSubstr(provided, "fakeRegistry"), "mock output listed")
	testza.AssertTrue(t, containsSubstr(provided, "dependentModule"), "module output listed")

	testza.AssertNil(t, s.Shutdown())
}

func TestSlice_WithTestLogger(t *testing.T) {
	t.Parallel()

	m := &loggingModule{inited: make(chan struct{})}
	s := testkit.NewSlice(t, m).WithTestLogger()
	s.Start()
	waitInit(t, m.inited)

	logger := testkit.Get[*slog.Logger](s)
	testza.AssertTrue(t, logger.Enabled(context.Background(), slog.LevelDebug))
	testza.AssertEqual(t, int32(1), m.initCalls.Load())
	testza.AssertNil(t, s.Shutdown())
}

func TestSlice_WithoutTestLogger_NoCrash(t *testing.T) {
	t.Parallel()

	m := &loggingModule{inited: make(chan struct{})}
	s := testkit.NewSlice(t, m)
	s.Start()
	waitInit(t, m.inited)

	testza.AssertEqual(t, int32(1), m.initCalls.Load())
	testza.AssertNil(t, s.Shutdown())
}

func TestSlice_WithConfig_HotReload(t *testing.T) {
	t.Parallel()

	m := &reloadableModule{started: make(chan struct{})}
	s := testkit.NewSlice(t, m).WithConfig(map[string]any{"foo": "bar"})
	s.Start()

	select {
	case <-m.started:
	case <-time.After(startTimeout):
		t.Fatal("module did not start")
	}

	newK := koanf.New(".")
	if err := newK.Load(testkit.MapProvider(map[string]any{"foo": "baz"}), nil); err != nil {
		t.Fatal(err)
	}
	s.Notifier().FireReload(newK)

	testza.AssertEqual(t, int32(1), m.reloaded.Load())
	testza.AssertNil(t, s.Shutdown())
}

func TestSlice_UndeclaredDep_EscapeHatch(t *testing.T) {
	t.Parallel()

	m := &undeclaredModule{}
	s := testkit.NewSlice(t, m)

	// Validate cannot see undeclared deps.
	testza.AssertNil(t, s.Validate())

	// Boot: Validate passes, but the undeclared Invoke fails at Init and surfaces
	// through Shutdown.
	s.Start()
	err := s.Shutdown()
	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, int32(1), m.initCalls.Load())
}
