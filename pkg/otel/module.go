package otel

import (
	"context"
	"reflect"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	otellog "go.opentelemetry.io/otel/log"
	nooplog "go.opentelemetry.io/otel/log/noop"
	otelmetric "go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	oteltrace "go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// Module manages OpenTelemetry SDK lifecycle.
type Module struct {
	lakta.NamedBase

	config    Config
	providers otelProviders
}

// NewModule creates a new OTEL module
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{
		NamedBase: lakta.NewNamedBase(cfg.Name),
		config:    cfg,
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryOTel, "otel", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Config returns the current module configuration.
func (m *Module) Config() Config {
	return m.config
}

// Init sets up the entire OTEL provider and exporter stack.
func (m *Module) Init(ctx context.Context) error {
	if !m.config.Enabled {
		m.providers.shutdown = func(context.Context) error { return nil }
		lakta.ProvideValue[oteltrace.TracerProvider](ctx, nooptrace.NewTracerProvider())
		lakta.ProvideValue[otelmetric.MeterProvider](ctx, noopmetric.NewMeterProvider())
		lakta.ProvideValue[otellog.LoggerProvider](ctx, nooplog.NewLoggerProvider())
		return nil
	}

	if m.config.SetupFn != nil {
		shutdown, err := m.config.SetupFn(ctx, m.config.ServiceName)
		if err != nil {
			return err
		}
		m.providers.shutdown = shutdown
		lakta.ProvideValue[oteltrace.TracerProvider](ctx, nooptrace.NewTracerProvider())
		lakta.ProvideValue[otelmetric.MeterProvider](ctx, noopmetric.NewMeterProvider())
		lakta.ProvideValue[otellog.LoggerProvider](ctx, nooplog.NewLoggerProvider())
		return nil
	}

	var err error
	m.providers, err = setupOTelSDK(ctx, m.config)
	if err != nil {
		return oops.Wrapf(err, "failed to setup OpenTelemetry SDK")
	}

	if m.providers.tracerProvider != nil {
		lakta.ProvideValue[oteltrace.TracerProvider](ctx, m.providers.tracerProvider)
	} else {
		lakta.ProvideValue[oteltrace.TracerProvider](ctx, nooptrace.NewTracerProvider())
	}
	if m.providers.meterProvider != nil {
		lakta.ProvideValue[otelmetric.MeterProvider](ctx, m.providers.meterProvider)
	} else {
		lakta.ProvideValue[otelmetric.MeterProvider](ctx, noopmetric.NewMeterProvider())
	}
	if m.providers.loggerProvider != nil {
		lakta.ProvideValue[otellog.LoggerProvider](ctx, m.providers.loggerProvider)
	} else {
		lakta.ProvideValue[otellog.LoggerProvider](ctx, nooplog.NewLoggerProvider())
	}

	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[oteltrace.TracerProvider](),
		reflect.TypeFor[otelmetric.MeterProvider](),
		reflect.TypeFor[otellog.LoggerProvider](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// Shutdown gracefully stops the OTEL exporters.
func (m *Module) Shutdown(ctx context.Context) error {
	return m.providers.shutdown(ctx)
}
