package temporal

import (
	"context"
	"log/slog"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	otelmetric "go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
)

const (
	defaultHealthInterval = 10 * time.Second
	defaultBackoff        = time.Second
	maxBackoff            = 30 * time.Second
	healthCheckTimeout    = 5 * time.Second
)

// stopper is the part of worker.Worker the run loop needs after a successful start.
type stopper interface{ Stop() }

// Module runs a Temporal worker without needing Temporal at boot: Init builds
// a lazy client (no dial) and validates the registrars; Start keeps trying to
// start the worker with backoff. The module is ready only once the worker has
// started and while periodic health checks pass; it exports
// temporal_worker_connected and temporal_dial_failures.
type Module struct {
	lakta.NamedBase

	config Config
	client client.Client

	tracing interceptor.Interceptor

	startWorker func(ctx context.Context) (stopper, error)
	checkHealth func(ctx context.Context) error

	started  atomic.Bool
	running  atomic.Bool
	healthy  atomic.Bool
	failures atomic.Int64
	stopped  chan struct{}

	dialFailures otelmetric.Int64Counter
}

// NewModule creates a new Temporal module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	m := &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
		stopped:   make(chan struct{}),
	}
	m.startWorker = m.defaultStartWorker
	m.checkHealth = m.defaultCheckHealth
	return m
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryWorkflows, "temporal", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[client.Client](),
	}
}

// Dependencies declares what this module uses when present, so it initializes
// after logging, telemetry and the health container.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[*slog.Logger](),
		reflect.TypeFor[oteltrace.TracerProvider](),
		reflect.TypeFor[otelmetric.MeterProvider](),
		reflect.TypeFor[*health.Health](),
	}
}

// Init builds the lazy client and checks that the registrars succeed on a
// throwaway worker. It never contacts Temporal.
func (m *Module) Init(ctx context.Context) error {
	if !m.config.Enabled {
		return nil
	}
	if m.config.TaskQueue == "" {
		return oops.Errorf("task queue is required in temporal configuration")
	}

	tracing, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{})
	if err != nil {
		return oops.Wrapf(err, "failed to create tracing interceptor")
	}
	m.tracing = tracing

	m.client, err = client.NewLazyClient(m.config.ClientOptions(slox.From(ctx), []interceptor.ClientInterceptor{tracing}))
	if err != nil {
		return oops.Wrapf(err, "failed to create Temporal client")
	}

	probe := worker.New(m.client, m.config.TaskQueue, m.config.WorkerOptions(ctx, []interceptor.WorkerInterceptor{tracing}))
	for _, register := range m.config.Registrars {
		if err := register(ctx, probe); err != nil {
			m.client.Close()
			return oops.Wrapf(err, "failed to register workflows and activities")
		}
	}

	if err := m.registerTelemetry(ctx); err != nil {
		m.client.Close()
		return err
	}

	lakta.ProvideValue[client.Client](ctx, m.client)

	return nil
}

func (m *Module) registerTelemetry(ctx context.Context) error {
	provider := otel.GetMeterProvider()
	if mp, err := do.Invoke[otelmetric.MeterProvider](lakta.GetInjector(ctx)); err == nil {
		provider = mp
	}
	meter := provider.Meter("github.com/Vilsol/lakta/pkg/workflows/temporal")

	var err error
	m.dialFailures, err = meter.Int64Counter("temporal_dial_failures",
		otelmetric.WithDescription("Failed attempts to start the Temporal worker."))
	if err != nil {
		return oops.Wrapf(err, "failed to create dial failure counter")
	}

	_, err = meter.Int64ObservableGauge("temporal_worker_connected",
		otelmetric.WithDescription("1 while the Temporal worker runs and Temporal answers health checks."),
		otelmetric.WithInt64Callback(func(ctx context.Context, o otelmetric.Int64Observer) error {
			if m.Ready(ctx) == nil {
				o.Observe(1)
			} else {
				o.Observe(0)
			}
			return nil
		}))
	if err != nil {
		return oops.Wrapf(err, "failed to create worker gauge")
	}

	if h, herr := do.Invoke[*health.Health](lakta.GetInjector(ctx)); herr == nil {
		if rerr := h.Register(health.Config{Name: "temporal-" + m.config.Name, Check: m.Ready}); rerr != nil {
			return oops.Wrapf(rerr, "failed to register readiness check")
		}
	}

	return nil
}

// Ready fails until the worker has started and whenever Temporal stops
// answering health checks.
func (m *Module) Ready(_ context.Context) error {
	if !m.running.Load() {
		return oops.Errorf("temporal worker is not running")
	}
	if !m.healthy.Load() {
		return oops.Errorf("temporal is not answering health checks")
	}
	return nil
}

// Start keeps trying to start the worker until it runs, then blocks until ctx
// is done and stops the worker. When disabled it only waits for ctx.
func (m *Module) Start(ctx context.Context) error {
	m.started.Store(true)
	defer close(m.stopped)

	if !m.config.Enabled {
		<-ctx.Done()
		return nil
	}

	go m.healthLoop(ctx)

	backoff := m.config.backoff
	for {
		w, err := m.startWorker(ctx)
		if err == nil {
			m.running.Store(true)
			<-ctx.Done()
			m.running.Store(false)
			w.Stop()
			return nil
		}

		m.failures.Add(1)
		if m.dialFailures != nil {
			m.dialFailures.Add(ctx, 1)
		}
		slox.Warn(ctx, "temporal worker failed to start, retrying",
			slog.Any("error", err), slog.Duration("retry_in", backoff))

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff) //nolint:mnd // exponential backoff
	}
}

func (m *Module) healthLoop(ctx context.Context) {
	for {
		checkCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
		m.healthy.Store(m.checkHealth(checkCtx) == nil)
		cancel()

		select {
		case <-ctx.Done():
			return
		case <-time.After(m.config.healthInterval):
		}
	}
}

func (m *Module) defaultStartWorker(ctx context.Context) (stopper, error) { //nolint:ireturn // narrow internal interface
	w := worker.New(m.client, m.config.TaskQueue, m.config.WorkerOptions(ctx, []interceptor.WorkerInterceptor{m.tracing}))
	for _, register := range m.config.Registrars {
		if err := register(ctx, w); err != nil {
			return nil, oops.Wrapf(err, "failed to register workflows and activities")
		}
	}
	if err := w.Start(); err != nil {
		return nil, oops.Wrapf(err, "failed to start worker")
	}
	return w, nil
}

func (m *Module) defaultCheckHealth(ctx context.Context) error {
	_, err := m.client.CheckHealth(ctx, &client.CheckHealthRequest{})
	return oops.Wrapf(err, "temporal health check failed")
}

// Shutdown waits for Start to stop the worker, then closes the client.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.started.Load() {
		select {
		case <-m.stopped:
		case <-ctx.Done():
			return oops.Wrapf(ctx.Err(), "temporal worker did not stop in time")
		}
	}
	if m.client != nil {
		m.client.Close()
	}
	return nil
}
