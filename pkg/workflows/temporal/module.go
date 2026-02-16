package temporal

import (
	"context"
	"errors"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

// Module manages a Temporal client and worker lifecycle.
type Module struct {
	config Config
	client client.Client
	worker worker.Worker
}

// NewModule creates a new Temporal module with the given options.
func NewModule(options ...Option) *Module {
	return &Module{config: NewConfig(options...)}
}

// Name returns the instance name.
func (m *Module) Name() string {
	return m.config.Name
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryWorkflows, "temporal", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	path := m.ConfigPath()
	if k.Exists(path) {
		return m.config.LoadFromKoanf(k, path)
	}
	return nil
}

// Init loads configuration and validates required fields.
func (m *Module) Init(ctx context.Context) error {
	// Load config from koanf if available
	if k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx)); err == nil {
		if err := m.LoadConfig(k); err != nil {
			return oops.Wrapf(err, "failed to load config")
		}
	}

	if m.config.TaskQueue == "" {
		return errors.New("task queue is required in temporal configuration")
	}

	return nil
}

// Start connects to Temporal, registers workflows/activities, and runs the worker.
func (m *Module) Start(ctx context.Context) error {
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
	defer m.client.Close()

	workerOpts := m.config.WorkerOptions(ctx, []interceptor.WorkerInterceptor{tracingInterceptor})
	m.worker = worker.New(m.client, m.config.TaskQueue, workerOpts)

	for _, register := range m.config.Registrars {
		if err := register(ctx, m.worker); err != nil {
			return err
		}
	}

	lakta.Provide(ctx, m.getClient)

	if err = m.worker.Run(worker.InterruptCh()); err != nil {
		return oops.Wrapf(err, "failed to start worker")
	}

	return nil
}

// Shutdown stops the worker and closes the Temporal client.
func (m *Module) Shutdown(_ context.Context) error {
	m.worker.Stop()
	m.client.Close()
	return nil
}

// getClient returns the Temporal client instance.
//
//nolint:ireturn
func (m *Module) getClient(_ do.Injector) (client.Client, error) {
	return m.client, nil
}
