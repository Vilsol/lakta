package policy

import (
	"context"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/failsafe-go/failsafe-go"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Module provides a [Registry] of named resilience policies via DI.
// Policies are static after Init: config hot-reload is deliberately not
// supported because circuit breakers hold runtime state that a reload would
// silently reset.
type Module struct {
	config   Config
	registry *Registry
}

// NewModule creates a new resilience policy module.
func NewModule(options ...Option) *Module {
	return &Module{
		config: NewConfig(options...),
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryResilience, "policy", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init builds the policy executors and provides the [Registry] to the injector.
func (m *Module) Init(ctx context.Context) error {
	executors := make(map[string]failsafe.Executor[any], len(m.config.CodePolicies)+len(m.config.Policies))

	for name, policies := range m.config.CodePolicies {
		if len(policies) == 0 {
			return oops.Errorf("resilience policy %q defines no primitives", name)
		}
		executors[name] = failsafe.With(policies...)
	}
	pm := newPolicyMetrics()
	for name, pc := range m.config.Policies {
		built, limiter, err := pc.buildWithMetrics(name, pm)
		if err != nil {
			return oops.Wrapf(err, "failed to build resilience policy %q", name)
		}
		executors[name] = failsafe.With(built...)
		if limiter != nil {
			if err := pm.registerLimiterGauges(name, limiter); err != nil {
				return oops.Wrapf(err, "failed to register limiter gauges for %q", name)
			}
		}
	}

	m.registry = &Registry{executors: executors}
	lakta.ProvideValue(ctx, m.registry)

	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*Registry](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown is a no-op for the resilience policy module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}
