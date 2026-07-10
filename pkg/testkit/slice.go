package testkit

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

// The two diagnostic strings are a DX contract: emit them verbatim, interpolating
// only the type/module names. Modules render via reflect.TypeOf (e.g. *foo.Module),
// matching %T; tests build their golden strings from these same constants.
const (
	// sliceIncompleteFmt args: missing type, requiring module, missing type.
	//   slice incomplete: *foo.Registry required by *foo.Module — add it, or Mock[*foo.Registry](s, ...)
	sliceIncompleteFmt = "slice incomplete: %v required by %v — add it, or Mock[%v](s, ...)"

	// mockCollisionFmt args: mocked type, colliding module.
	//   mock for *foo.Registry collides with module *foo.Module which also provides it
	mockCollisionFmt = "mock for %v collides with module %v which also provides it"
)

// Slice boots a subset of modules with mocked collaborators, surfacing unmet
// declared dependencies as pre-boot diagnostics instead of a raw topo-sort error
// inside Init. It owns a pre-seeded injector so Mock/SliceProvide doubles survive
// into module Init (relies on RunContext adopting a ctx-supplied injector).
type Slice struct {
	t        *testing.T
	injector do.Injector
	ctx      context.Context //nolint:containedctx
	modules  []lakta.Module
	seeded   map[reflect.Type]struct{}
	notifier *ReloadNotifier
	harness  *RuntimeHarness
}

// --- construction ---

// NewSlice creates a Slice for the given modules under test.
func NewSlice(t *testing.T, modules ...lakta.Module) *Slice {
	t.Helper()
	injector := do.New()

	return &Slice{
		t:        t,
		injector: injector,
		ctx:      lakta.WithInjector(context.Background(), injector),
		modules:  modules,
		seeded:   make(map[reflect.Type]struct{}),
		notifier: &ReloadNotifier{},
	}
}

// With appends more modules under test.
func (s *Slice) With(modules ...lakta.Module) *Slice {
	s.modules = append(s.modules, modules...)
	return s
}

// --- wiring ---

// WithConfig builds a koanf from data and registers *koanf.Koanf +
// config.ReloadNotifier (the testkit *ReloadNotifier) in the slice injector,
// reusing the MapProvider path from Harness.WithData.
func (s *Slice) WithConfig(data map[string]any) *Slice {
	s.t.Helper()
	k := koanf.New(".")
	if err := k.Load(MapProvider(data), nil); err != nil {
		s.t.Fatal(err)
	}

	do.Provide(s.injector, func(_ do.Injector) (*koanf.Koanf, error) {
		return k, nil
	})
	do.Provide(s.injector, func(_ do.Injector) (config.ReloadNotifier, error) {
		return s.notifier, nil
	})

	return s
}

// WithTestLogger registers a *slog.Logger bridging records to t.Log, so the
// runtime's do.Invoke[*slog.Logger] picks it up instead of slog.Default().
func (s *Slice) WithTestLogger() *Slice {
	h := slog.NewTextHandler(testWriter{s.t}, &slog.HandlerOptions{Level: slog.LevelDebug})
	do.ProvideValue(s.injector, slog.New(h))
	return s
}

// Mock seeds a double for an unmet dependency of type T. Excluding a module and
// mocking its output are the same operation (do/v2 Provide panics on duplicates).
// Free fn: Go has no generic methods.
func Mock[T any](s *Slice, v T) *Slice {
	s.seeded[reflect.TypeFor[T]()] = struct{}{}
	do.ProvideValue(s.injector, v)
	return s
}

// SliceProvide registers an arbitrary provider into the slice injector
// (mirrors testkit.WithProvider). Free fn: Go has no generic methods.
func SliceProvide[T any](s *Slice, provider func(do.Injector) (T, error)) *Slice {
	s.seeded[reflect.TypeFor[T]()] = struct{}{}
	do.Provide(s.injector, provider)
	return s
}

// --- lifecycle ---

// Validate runs the two pre-boot checks and returns the first failure: unmet
// declared deps (against every module's Provides() plus seeded mocks), cycles
// (delegated to Runtime.Validate, ignoring its unmet-dep verdict), and mock/module
// collisions (a seeded type a module-under-test also Provides()).
func (s *Slice) Validate() error {
	satisfiable := s.satisfiableTypes()
	for _, m := range s.modules {
		d, ok := m.(lakta.Dependent)
		if !ok {
			continue
		}

		required, _ := d.Dependencies()
		for _, t := range required {
			if _, ok := satisfiable[t]; !ok {
				return oops.Errorf(sliceIncompleteFmt, t, reflect.TypeOf(m), t)
			}
		}
	}

	// Cycles only: mocks are leaves and cannot participate, so run Validate over the
	// module set and ignore its unmet-dep verdict (already handled above with mocks).
	if err := lakta.NewRuntime(s.modules...).Validate(); err != nil && !errors.Is(err, lakta.ErrUnmetDependency) {
		return oops.Wrapf(err, "validating slice module graph")
	}

	for t := range s.seeded {
		if owner := s.moduleProviding(t); owner != nil {
			return oops.Errorf(mockCollisionFmt, t, reflect.TypeOf(owner))
		}
	}

	return nil
}

// Start calls Validate (t.Fatal on error, before any module Init), then boots the
// runtime through the ctx-accepting RuntimeHarness entry using the slice's
// pre-seeded injector context.
func (s *Slice) Start() *Slice {
	s.t.Helper()
	if err := s.Validate(); err != nil {
		s.t.Fatal(err)
	}

	s.harness = newRuntimeHarnessCtx(s.t, s.ctx, s.modules...)
	return s
}

// Shutdown delegates to the underlying RuntimeHarness.Shutdown.
func (s *Slice) Shutdown() error {
	return s.harness.Shutdown()
}

// satisfiableTypes returns every type any module-under-test Provides() plus every
// Mock/SliceProvide type.
func (s *Slice) satisfiableTypes() map[reflect.Type]struct{} {
	out := make(map[reflect.Type]struct{}, len(s.seeded))
	for t := range s.seeded {
		out[t] = struct{}{}
	}

	for _, m := range s.modules {
		if p, ok := m.(lakta.Provider); ok {
			for _, t := range p.Provides() {
				out[t] = struct{}{}
			}
		}
	}

	return out
}

// moduleProviding returns the module-under-test that Provides() t, or nil.
func (s *Slice) moduleProviding(t reflect.Type) lakta.Module { //nolint:ireturn
	for _, m := range s.modules {
		p, ok := m.(lakta.Provider)
		if !ok {
			continue
		}

		if slices.Contains(p.Provides(), t) {
			return m
		}
	}

	return nil
}

// --- access ---

// Get invokes T from the slice injector or calls t.Fatal. Free fn.
func Get[T any](s *Slice) T { //nolint:ireturn
	s.t.Helper()
	v, err := do.Invoke[T](s.injector)
	if err != nil {
		s.t.Fatal(err)
	}
	return v
}

// Ctx returns the context carrying the slice injector.
func (s *Slice) Ctx() context.Context {
	return s.ctx
}

// Injector returns the slice DI injector.
func (s *Slice) Injector() do.Injector { //nolint:ireturn
	return s.injector
}

// Provided wraps injector.ListProvidedServices(), returning service names.
func (s *Slice) Provided() []string {
	services := s.injector.ListProvidedServices()
	names := make([]string, len(services))
	for i, svc := range services {
		names[i] = svc.Service
	}
	return names
}

// Notifier returns the shared test ReloadNotifier (for FireReload in tests).
func (s *Slice) Notifier() *ReloadNotifier {
	return s.notifier
}

// testWriter adapts t.Log to io.Writer so a slog text handler attributes output
// to the test and stays silent on pass.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
