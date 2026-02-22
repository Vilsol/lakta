package otel

import (
	"context"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	protocolGRPC         = "grpc"
	protocolHTTPProtobuf = "http/protobuf"
	protocolHTTPJSON     = "http/json"

	signalTraces  = "traces"
	signalMetrics = "metrics"
	signalLogs    = "logs"

	defaultMetricIntervalSeconds = 60
)

// Config represents configuration for OTEL [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// ServiceName specifies the OpenTelemetry service name.
	ServiceName string `koanf:"service_name"`

	// ServiceVersion is included as semconv.ServiceVersionKey in the resource.
	ServiceVersion string `koanf:"service_version"`

	// ServiceNamespace is included as semconv.ServiceNamespaceKey in the resource.
	ServiceNamespace string `koanf:"service_namespace"`

	// Environment is the deployment environment (e.g. "production", "staging").
	Environment string `koanf:"environment"`

	// Endpoint overrides the OTLP exporter endpoint. Empty uses the SDK default (env vars).
	Endpoint string `koanf:"endpoint"`

	// Protocol sets the OTLP transport: "grpc" (default), "http/protobuf", or "http/json".
	Protocol string `koanf:"protocol"`

	// Insecure disables TLS on the OTLP connection — useful for local collectors.
	Insecure bool `koanf:"insecure"`

	// Headers are additional headers sent with every OTLP export (e.g. auth tokens).
	Headers map[string]string `koanf:"headers"`

	// SampleRate sets the trace sampling ratio. 1.0 = always sample, 0.0 = never sample.
	SampleRate float64 `koanf:"sample_rate"`

	// MetricInterval sets the periodic metric export interval.
	MetricInterval time.Duration `koanf:"metric_interval"`

	// RuntimeInterval sets the minimum Go runtime stats collection interval.
	RuntimeInterval time.Duration `koanf:"runtime_interval"`

	// Enabled controls whether OTEL is set up. When false, noop providers are registered.
	Enabled bool `koanf:"enabled"`

	// Signals lists which telemetry signals to enable: "traces", "metrics", "logs".
	Signals []string `koanf:"signals"`

	// SetupFn overrides the default OTLP SDK setup. Useful for testing or custom exporters.
	SetupFn func(ctx context.Context, serviceName string) (func(context.Context) error, error) `koanf:"-"`

	// TraceExporter overrides the trace exporter; takes precedence over Protocol selection.
	TraceExporter sdktrace.SpanExporter `koanf:"-"`

	// MetricExporter overrides the metric exporter; takes precedence over Protocol selection.
	MetricExporter sdkmetric.Exporter `koanf:"-"`

	// LogExporter overrides the log exporter; takes precedence over Protocol selection.
	LogExporter sdklog.Exporter `koanf:"-"`

	// Propagators overrides the default composite text map propagator (TraceContext + Baggage).
	Propagators []propagation.TextMapPropagator `koanf:"-"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:            config.DefaultInstanceName,
		ServiceName:     "lakta",
		Protocol:        protocolGRPC,
		SampleRate:      1.0,
		MetricInterval:  defaultMetricIntervalSeconds * time.Second,
		RuntimeInterval: time.Second,
		Enabled:         true,
		Signals:         []string{signalTraces, signalMetrics, signalLogs},
		Propagators:     []propagation.TextMapPropagator{propagation.TraceContext{}, propagation.Baggage{}},
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return config.Apply(NewDefaultConfig(), options...)
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithServiceName sets the OpenTelemetry service name.
func WithServiceName(name string) Option {
	return func(m *Config) { m.ServiceName = name }
}

// WithServiceVersion sets the service version included in the resource.
func WithServiceVersion(version string) Option {
	return func(m *Config) { m.ServiceVersion = version }
}

// WithServiceNamespace sets the service namespace included in the resource.
func WithServiceNamespace(ns string) Option {
	return func(m *Config) { m.ServiceNamespace = ns }
}

// WithEnvironment sets the deployment environment (e.g. "production").
func WithEnvironment(env string) Option {
	return func(m *Config) { m.Environment = env }
}

// WithEndpoint sets the OTLP exporter endpoint URL.
func WithEndpoint(endpoint string) Option {
	return func(m *Config) { m.Endpoint = endpoint }
}

// WithProtocol sets the OTLP transport protocol ("grpc", "http/protobuf", "http/json").
func WithProtocol(protocol string) Option {
	return func(m *Config) { m.Protocol = protocol }
}

// WithInsecure disables TLS for the OTLP connection.
func WithInsecure(insecure bool) Option {
	return func(m *Config) { m.Insecure = insecure }
}

// WithHeaders sets additional OTLP export headers (e.g. auth tokens).
func WithHeaders(headers map[string]string) Option {
	return func(m *Config) { m.Headers = headers }
}

// WithSampleRate sets the trace sampling ratio (0.0–1.0).
func WithSampleRate(rate float64) Option {
	return func(m *Config) { m.SampleRate = rate }
}

// WithMetricInterval sets the periodic metric export interval.
func WithMetricInterval(d time.Duration) Option {
	return func(m *Config) { m.MetricInterval = d }
}

// WithRuntimeInterval sets the minimum Go runtime stats collection interval.
func WithRuntimeInterval(d time.Duration) Option {
	return func(m *Config) { m.RuntimeInterval = d }
}

// WithEnabled enables or disables OTEL setup. When false, noop providers are registered.
func WithEnabled(enabled bool) Option {
	return func(m *Config) { m.Enabled = enabled }
}

// WithSignals sets which telemetry signals to enable ("traces", "metrics", "logs").
func WithSignals(signals ...string) Option {
	return func(m *Config) { m.Signals = signals }
}

// WithSetupFn overrides the default OTLP SDK setup function.
func WithSetupFn(fn func(ctx context.Context, serviceName string) (func(context.Context) error, error)) Option {
	return func(m *Config) { m.SetupFn = fn }
}

// WithTraceExporter sets a custom trace exporter, bypassing the Protocol-based selection.
func WithTraceExporter(e sdktrace.SpanExporter) Option {
	return func(m *Config) { m.TraceExporter = e }
}

// WithMetricExporter sets a custom metric exporter, bypassing the Protocol-based selection.
func WithMetricExporter(e sdkmetric.Exporter) Option {
	return func(m *Config) { m.MetricExporter = e }
}

// WithLogExporter sets a custom log exporter, bypassing the Protocol-based selection.
func WithLogExporter(e sdklog.Exporter) Option {
	return func(m *Config) { m.LogExporter = e }
}

// WithPropagators sets the text map propagators (default: TraceContext + Baggage).
func WithPropagators(propagators ...propagation.TextMapPropagator) Option {
	return func(m *Config) { m.Propagators = propagators }
}
