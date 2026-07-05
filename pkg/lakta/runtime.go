package lakta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"runtime/debug"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"github.com/sourcegraph/conc/pool"
)

const DefaultShutdownTimeout = 30 * time.Second

// Runtime orchestrates module initialization, startup, and shutdown.
type Runtime struct {
	modules []Module
}

// NewRuntime creates a runtime with the given modules. Order does not matter —
// the runtime resolves Init order automatically from Provider/Dependent declarations.
func NewRuntime(modules ...Module) *Runtime {
	return &Runtime{
		modules: modules,
	}
}

// Run starts the runtime, handling SIGTERM/SIGINT for graceful shutdown.
func (r *Runtime) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return r.RunContext(ctx)
}

// RunContext initializes, starts, and manages graceful shutdown of all modules.
// ctx cancellation is the shutdown trigger — callers are responsible for signal handling.
func (r *Runtime) RunContext(ctx context.Context) error {
	sorted, meta, err := sortModules(r.modules)
	if err != nil {
		return err
	}

	// Adopt a ctx-supplied injector (harness/test-slice mocks) instead of
	// overwriting it; without one, behavior is unchanged (fresh do.New).
	injector, ok := tryInjector(ctx)
	if !ok {
		injector = do.New()
		ctx = WithInjector(ctx, injector)
	}

	// Provided before the Init loop so later-initializing modules (the
	// actuator) can Invoke[*RuntimeInfo].
	info := &RuntimeInfo{modules: describeModules(sorted, meta)}
	ProvideValue(ctx, info)

	initialized, err := r.initModules(ctx, sorted, injector, info)
	if err != nil {
		return err
	}

	// Wire HotReloadable modules to the ReloadNotifier.
	if notifier, err := do.Invoke[ReloadNotifier](injector); err == nil {
		for _, module := range initialized {
			if hr, ok := module.(HotReloadable); ok {
				notifier.OnReload(hr.OnReload)
			}
			if v, ok := module.(ValidatableModule); ok {
				notifier.OnValidate(v.ValidateReload)
			}
		}
	}

	// Inject logger into context
	logger, err := do.Invoke[*slog.Logger](injector)
	if err != nil || logger == nil {
		slox.Warn(ctx, "failed to retrieve logger, continuing with default logger", slog.Any("error", err))

		logger = slog.Default()
		do.Provide(injector, func(_ do.Injector) (*slog.Logger, error) {
			return logger, nil
		})
	}

	ctx = slox.Into(ctx, logger)

	// Phase 1: Start async modules (non-blocking setup).
	asyncPool := pool.New().
		WithErrors().
		WithContext(ctx).
		WithCancelOnError()

	for order, module := range sorted {
		name := fmt.Sprintf("%T", module)

		switch m := module.(type) {
		case AsyncModule:
			asyncPool.Go(func(ctx context.Context) error {
				info.setState(order, StateStarted)

				if err := safeCall(func() error { return m.StartAsync(ctx) }); err != nil {
					slox.Error(ctx, "failed starting async module",
						slog.String("name", name), slog.Any("error", err))

					return oops.With("name", name).Wrapf(err, "failed starting module")
				}

				return nil
			})
		case SyncModule:
			// handled in phase 2
		default:
			slox.Debug(ctx, "skipping starting module without any start function",
				slog.String("name", name))
		}
	}

	if err := asyncPool.Wait(); err != nil {
		slox.Error(ctx, "async modules failed", slog.Any("error", err))

		shutdownTimeout, cancel := shutdownContext(ctx)
		defer cancel()

		if shutdownErr := r.shutdown(shutdownTimeout, initialized, info); shutdownErr != nil {
			return shutdownErr
		}

		return oops.Wrapf(err, "async modules failed")
	}

	// Phase 2: Start sync modules. The first sync module to return (clean OR error)
	// records the originating cause and cancels syncCtx; siblings observe the
	// cancellation and return, then the runtime proceeds to graceful shutdown.
	syncCtx, cancelSync := context.WithCancel(ctx)
	defer cancelSync()

	var (
		firstExit sync.Once
		firstErr  error
	)

	syncPool := pool.New().
		WithErrors().
		WithContext(syncCtx).
		WithCancelOnError()

	hasSyncModules := false

	for order, module := range sorted {
		if m, ok := module.(SyncModule); ok {
			hasSyncModules = true
			name := fmt.Sprintf("%T", module)

			syncPool.Go(func(ctx context.Context) error {
				info.setState(order, StateStarted)

				if cs, ok := m.(contextSetter); ok {
					cs.setCtx(ctx)
				}

				err := safeCall(func() error { return m.Start(ctx) })

				// First to return records the cause and cancels siblings; later
				// returns (often context.Canceled from this cancellation) are ignored.
				firstExit.Do(func() {
					firstErr = err
					cancelSync()
				})

				if err != nil {
					slox.Error(ctx, "failed starting sync module",
						slog.String("name", name), slog.Any("error", err))

					return oops.With("name", name).Wrapf(err, "failed starting module")
				}

				return nil
			})
		}
	}

	if hasSyncModules {
		syncDone := make(chan struct{})
		go func() {
			_ = syncPool.Wait()
			close(syncDone)
		}()

		select {
		case <-ctx.Done():
			slox.Info(ctx, "shutdown signal received")
		case <-syncDone:
			// A sync module returned, so we are shutting down regardless. Surface the
			// originating cause only if it was a genuine failure — not a clean exit
			// and not a parent-context cancellation (SIGTERM).
			if firstErr != nil && ctx.Err() == nil {
				slox.Error(ctx, "sync module failed", slog.Any("error", firstErr))

				shutdownTimeout, cancel := shutdownContext(ctx)
				defer cancel()

				if shutdownErr := r.shutdown(shutdownTimeout, initialized, info); shutdownErr != nil {
					return shutdownErr
				}

				return oops.Wrapf(firstErr, "sync module failed")
			}

			slox.Info(ctx, "a sync module stopped, shutting down")
		}
	} else {
		<-ctx.Done()
		slox.Info(ctx, "shutdown signal received")
	}

	shutdownTimeout, cancel := shutdownContext(ctx)
	defer cancel()

	return r.shutdown(shutdownTimeout, initialized, info)
}

// initModules initializes modules sequentially in dependency order, loading
// config for Configurable modules first and recording state/duration in info.
// On failure it tears down the already-initialized prefix and returns the
// wrapped error.
func (r *Runtime) initModules(ctx context.Context, sorted []Module, injector do.Injector, info *RuntimeInfo) ([]Module, error) {
	initialized := make([]Module, 0, len(sorted))

	for order, module := range sorted {
		name := fmt.Sprintf("%T", module)

		if c, ok := module.(Configurable); ok {
			k, kErr := do.Invoke[*koanf.Koanf](injector)
			if kErr == nil && k.Exists(c.ConfigPath()) {
				if err := c.LoadConfig(k); err != nil {
					slox.Error(ctx, "failed loading config for module", slog.String("name", name), slog.Any("error", err))
					info.setState(order, StateFailed)
					r.teardown(ctx, initialized, info)

					return nil, oops.
						With("name", name).
						Wrapf(err, "failed loading config for module")
				}
			}
		}

		initStart := time.Now()
		if err := safeCall(func() error { return module.Init(ctx) }); err != nil {
			slox.Error(ctx, "failed initializing module", slog.Any("error", err))
			info.setState(order, StateFailed)
			r.teardown(ctx, initialized, info)

			return nil, oops.
				With("name", name).
				Wrapf(err, "failed initializing module")
		}

		info.setInitDuration(order, time.Since(initStart))
		info.setState(order, StateInitialized)

		initialized = append(initialized, module)
	}

	return initialized, nil
}

// shutdownContext derives a fresh timeout context for shutdown. It preserves
// ctx values (logger, injector) but is detached from ctx cancellation, which is
// typically already triggered by the time shutdown runs.
func shutdownContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), DefaultShutdownTimeout)
}

// safeCall runs fn, converting any panic into an error with a stack trace.
func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = oops.With("stack", string(debug.Stack())).Errorf("panic: %v", r)
		}
	}()
	return fn()
}

// shutdownModule runs module.Shutdown in a goroutine and races it against ctx.
// On deadline expiry the goroutine is abandoned (the process is exiting) and a
// timeout error is returned. Panics inside Shutdown are recovered into an error.
func shutdownModule(ctx context.Context, module Module) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- oops.With("stack", string(debug.Stack())).Errorf("panic during shutdown: %v", r)
			}
		}()
		done <- module.Shutdown(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "shutdown deadline exceeded")
	}
}

// teardown shuts down modules in reverse order under a fresh deadline, logging
// but not returning errors. Used when cleaning up after an Init failure.
func (r *Runtime) teardown(ctx context.Context, initialized []Module, info *RuntimeInfo) {
	timeoutCtx, cancel := shutdownContext(ctx)
	defer cancel()

	for order, module := range slices.Backward(initialized) {
		name := fmt.Sprintf("%T", module)

		if timeoutCtx.Err() != nil {
			slox.Error(ctx, "shutdown deadline exceeded, skipping module teardown", slog.String("name", name))
			continue
		}

		if err := shutdownModule(timeoutCtx, module); err != nil {
			slox.Error(ctx, "failed shutting down module", slog.String("name", name), slog.Any("error", err))
			continue
		}

		info.setState(order, StateStopped)
	}
}

// shutdown shuts down modules in reverse order, returning the first error.
// Modules remaining after the deadline expires are logged and skipped.
// initialized is a prefix of the topo-sorted slice, so its index is the
// module's InitOrder.
func (r *Runtime) shutdown(ctx context.Context, initialized []Module, info *RuntimeInfo) error {
	var firstErr error

	for order, module := range slices.Backward(initialized) {
		name := fmt.Sprintf("%T", module)

		if ctx.Err() != nil {
			slox.Error(ctx, "shutdown deadline exceeded, skipping module", slog.String("name", name))
			if firstErr == nil {
				firstErr = oops.With("name", name).Wrapf(ctx.Err(), "shutdown deadline exceeded")
			}
			continue
		}

		if err := shutdownModule(ctx, module); err != nil {
			slox.Error(ctx, "failed shutting down module", slog.String("name", name), slog.Any("error", err))
			if firstErr == nil {
				firstErr = oops.With("name", name).Wrapf(err, "failed shutting down module")
			}
			continue
		}

		info.setState(order, StateStopped)
	}

	return firstErr
}

// ErrUnmetDependency is the typed sentinel for a module declaring a required
// dependency no other module Provides(). Unmet-dep errors wrap it (oops, with
// module/type detail) so callers match via errors.Is instead of string-matching.
var ErrUnmetDependency = errors.New("unmet module dependency")

// moduleMeta is the metadata sortModules derives while building the dependency
// graph. Threaded out (indexed parallel to the sorted slice) so describeModules
// reuses it instead of re-calling Provides()/Dependencies().
type moduleMeta struct {
	provides []reflect.Type
	required []reflect.Type
	optional []reflect.Type
}

// Validate runs the dependency topo-sort only — no do.New, no Init, no side
// effects. Catches cycles and unmet declared required deps up front.
// Limitation: undeclared do.MustInvoke calls are invisible here and only
// surface at Init.
func (r *Runtime) Validate() error {
	_, _, err := sortModules(r.modules)
	return err
}

// describeModules builds the initial []ModuleInfo (State = StatePending) from
// the sorted modules and their meta, assigning InitOrder = index.
func describeModules(sorted []Module, meta []moduleMeta) []ModuleInfo {
	infos := make([]ModuleInfo, len(sorted))

	for i, m := range sorted {
		var name string
		if nm, ok := m.(NamedModule); ok {
			name = nm.Name()
		}

		infos[i] = ModuleInfo{
			Name:      name,
			Type:      fmt.Sprintf("%T", m),
			InitOrder: i,
			Provides:  renderTypes(meta[i].provides),
			Requires:  renderTypes(meta[i].required),
			Optional:  renderTypes(meta[i].optional),
			Lifecycle: lifecycleOf(m),
			State:     StatePending,
		}
	}

	return infos
}

// sortModules topologically sorts modules based on Provider/Dependent declarations
// using Kahn's algorithm. Modules with no declared deps preserve their original order.
// meta[i] holds the declarations derived for sorted[i], so callers avoid a second
// reflect pass. Unmet required deps satisfy errors.Is(err, ErrUnmetDependency).
func sortModules(modules []Module) ([]Module, []moduleMeta, error) {
	// Build type → module index map from Provider declarations
	typeOwner := make(map[reflect.Type]int)
	metaByIdx := make([]moduleMeta, len(modules))

	for i, m := range modules {
		p, ok := m.(Provider)
		if !ok {
			continue
		}

		metaByIdx[i].provides = p.Provides()

		for _, t := range metaByIdx[i].provides {
			typeOwner[t] = i
		}
	}

	// Build adjacency list and in-degree count
	// edges[i] = list of module indices that must init after module i
	edges := make([][]int, len(modules))
	inDegree := make([]int, len(modules))

	for i, m := range modules {
		d, ok := m.(Dependent)
		if !ok {
			continue
		}

		required, optional := d.Dependencies()
		metaByIdx[i].required = required
		metaByIdx[i].optional = optional

		for _, t := range required {
			ownerIdx, found := typeOwner[t]
			if !found {
				return nil, nil, oops.
					With("module", fmt.Sprintf("%T", m)).
					With("type", t.String()).
					Wrapf(ErrUnmetDependency, "module %T requires type %v but no module provides it", m, t)
			}

			if ownerIdx == i {
				continue
			}

			edges[ownerIdx] = append(edges[ownerIdx], i)
			inDegree[i]++
		}

		for _, t := range optional {
			ownerIdx, found := typeOwner[t]
			if !found {
				continue
			}

			if ownerIdx == i {
				continue
			}

			edges[ownerIdx] = append(edges[ownerIdx], i)
			inDegree[i]++
		}
	}

	// Kahn's algorithm — seed queue with all zero-in-degree modules in original order
	queue := make([]int, 0, len(modules))

	for i := range modules {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	sorted := make([]Module, 0, len(modules))
	meta := make([]moduleMeta, 0, len(modules))

	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		sorted = append(sorted, modules[idx])
		meta = append(meta, metaByIdx[idx])

		for _, next := range edges[idx] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(sorted) != len(modules) {
		return nil, nil, oops.Errorf("cycle detected in module dependencies")
	}

	return sorted, meta, nil
}
