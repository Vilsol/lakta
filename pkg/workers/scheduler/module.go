package scheduler

import (
	"context"
	"log/slog"
	"maps"
	"reflect"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/go-co-op/gocron/v2" // verified v2.21.2
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	otelmetric "go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// tracerName identifies this module's tracer.
const tracerName = "github.com/Vilsol/lakta/pkg/workers/scheduler"

// Module wires a [Scheduler] into DI as an AsyncModule.
type Module struct {
	lakta.NamedBase

	config Config
	sched  *Scheduler
}

// NewModule creates a new worker-scheduler module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryWorkers, "scheduler", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init builds the gocron scheduler, registers every merged job, and provides
// the [Scheduler] to the injector so app modules can Register more jobs during
// their own Init (topo-sort guarantees scheduler inits first when declared a dep).
func (m *Module) Init(ctx context.Context) error {
	loc, err := time.LoadLocation(m.config.Timezone)
	if err != nil {
		return oops.Wrapf(err, "invalid scheduler timezone %q", m.config.Timezone)
	}

	cron, err := gocron.NewScheduler(gocron.WithLocation(loc)) // verified v2.21.2: WithLocation takes *time.Location
	if err != nil {
		return oops.Wrapf(err, "failed to create scheduler")
	}

	m.sched = &Scheduler{
		cron:   cron,
		tracer: optionalTracer(ctx),
		defTZ:  m.config.Timezone,
		runCtx: context.WithoutCancel(ctx),
		jobs:   make(map[string]gocron.Job),
		specs:  make(map[string]JobSpec),
	}

	for name, spec := range m.config.MergedJobs() {
		if err := m.sched.Register(name, spec); err != nil {
			return oops.Wrapf(err, "failed to register job %q", name)
		}
	}

	lakta.ProvideValue(ctx, m.sched)

	return nil
}

// StartAsync starts the gocron scheduler (non-blocking).
func (m *Module) StartAsync(_ context.Context) error {
	m.sched.cron.Start() // verified v2.21.2: Start() is non-blocking

	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*Scheduler](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
// The otel module always provides a MeterProvider (noop when disabled), so
// declaring it orders otel before the scheduler; the tracer is resolved
// analogously and falls back to noop when otel is absent entirely.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[otelmetric.MeterProvider](),
	}
}

// Shutdown stops gocron (blocks until in-flight jobs finish) raced against ctx,
// mirroring pool.awaitClose: on the deadline it returns a wrapped ctx error.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.sched == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- m.sched.cron.Shutdown() }() // verified v2.21.2: Shutdown() blocks until in-flight jobs finish

	select {
	case err := <-done:
		return oops.Wrapf(err, "failed to shut down scheduler")
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "timed out draining scheduler")
	}
}

// OnReload re-loads config then diffs MergedJobs against the live specs: added
// names get Registered, removed names get removed, and any name whose
// schedule/tz/jitter/overlap/enabled changed is re-Registered. Handlers are
// code-owned and persist across reload (config never carries a func).
func (m *Module) OnReload(k *koanf.Koanf) {
	ctx := m.sched.runCtx

	reloaded := NewDefaultConfig()
	reloaded.Name = m.config.Name
	reloaded.CodeJobs = m.config.CodeJobs

	if err := reloaded.LoadFromKoanf(k, m.ConfigPath()); err != nil {
		slox.Error(ctx, "failed to reload scheduler config", slog.Any("error", err))
		return
	}

	m.config = reloaded
	desired := reloaded.MergedJobs()

	m.sched.mu.Lock()
	live := maps.Clone(m.sched.specs)
	m.sched.mu.Unlock()

	for name := range live {
		if _, ok := desired[name]; !ok {
			if err := m.sched.remove(name); err != nil {
				slox.Error(ctx, "failed to remove job on reload", slog.String("job", name), slog.Any("error", err))
			}
		}
	}

	for name, spec := range desired {
		old, existed := live[name]
		if existed && !specChanged(old, spec) {
			continue
		}

		if err := m.sched.Register(name, spec); err != nil {
			slox.Error(ctx, "failed to (re)register job on reload", slog.String("job", name), slog.Any("error", err))
		}
	}
}

// specChanged reports whether any config-derived field of two specs differs.
// Handler is code-owned and excluded (funcs are not comparable anyway).
func specChanged(a, b JobSpec) bool {
	return a.Schedule != b.Schedule ||
		a.Timezone != b.Timezone ||
		a.Jitter != b.Jitter ||
		a.Overlap != b.Overlap ||
		a.enabled() != b.enabled()
}

// optionalTracer resolves a Tracer from DI's TracerProvider, or a noop tracer
// when otel is absent.
func optionalTracer(ctx context.Context) oteltrace.Tracer { //nolint:ireturn // oteltrace.Tracer is the library interface
	if tp, err := lakta.Invoke[oteltrace.TracerProvider](ctx); err == nil {
		return tp.Tracer(tracerName)
	}

	return nooptrace.NewTracerProvider().Tracer(tracerName)
}
