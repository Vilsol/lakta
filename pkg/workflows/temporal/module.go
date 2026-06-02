package temporal

import (
	"context"
	"reflect"
	"sync"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
)

// Module manages a Temporal client and worker lifecycle.
type Module struct {
	lakta.NamedBase

	config Config
	client client.Client
	worker worker.Worker

	// interruptCh is closed to signal worker.Run to stop the worker.
	interruptCh chan any
	// stopOnce guards interruptCh closure so worker stop happens exactly once.
	stopOnce sync.Once
	// closeOnce guards client.Close so it happens exactly once.
	closeOnce sync.Once
}

// NewModule creates a new Temporal module with the given options.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase:   lakta.NewNamedBase(cfg.Name),
		config:      cfg,
		interruptCh: make(chan any),
	}
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

// Dependencies declares the types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, nil
}

// Init connects to Temporal and registers the client in DI before Start runs.
func (m *Module) Init(ctx context.Context) error {
	if m.config.TaskQueue == "" {
		return oops.Errorf("task queue is required in temporal configuration")
	}

	tracingInterceptor, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{})
	if err != nil {
		return oops.Wrapf(err, "failed to create tracing interceptor")
	}

	clientOpts := m.config.ClientOptions(
		slox.From(ctx),
		[]interceptor.ClientInterceptor{tracingInterceptor},
	)
	m.client, err = client.Dial(clientOpts)
	if err != nil {
		return oops.Wrapf(err, "failed to connect to Temporal")
	}

	m.worker = worker.New(m.client, m.config.TaskQueue, m.config.WorkerOptions(ctx, []interceptor.WorkerInterceptor{tracingInterceptor}))

	for _, register := range m.config.Registrars {
		if err := register(ctx, m.worker); err != nil {
			m.closeClient()
			return oops.Wrapf(err, "failed to register workflows and activities")
		}
	}

	lakta.ProvideValue[client.Client](ctx, m.client)

	return nil
}

// Start runs the worker, blocking until the runtime context is cancelled.
func (m *Module) Start(ctx context.Context) error {
	// Watch the runtime context: cancellation closes interruptCh so worker.Run unblocks.
	go func() {
		select {
		case <-ctx.Done():
			m.signalStop()
		case <-m.interruptCh:
		}
	}()

	if err := m.worker.Run(m.interruptCh); err != nil {
		m.closeClient()
		return oops.Wrapf(err, "failed to start worker")
	}

	return nil
}

// Shutdown stops the worker and closes the Temporal client.
func (m *Module) Shutdown(_ context.Context) error {
	m.signalStop()
	m.closeClient()
	return nil
}

// signalStop closes interruptCh once, unblocking worker.Run which stops the worker.
func (m *Module) signalStop() {
	m.stopOnce.Do(func() {
		close(m.interruptCh)
	})
}

// closeClient closes the Temporal client at most once.
func (m *Module) closeClient() {
	m.closeOnce.Do(func() {
		if m.client != nil {
			m.client.Close()
		}
	})
}
