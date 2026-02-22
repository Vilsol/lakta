package otel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type noopMetricExporter struct{}

func (noopMetricExporter) Temporality(_ sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}
func (noopMetricExporter) Aggregation(_ sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.AggregationDrop{}
}
func (noopMetricExporter) Export(_ context.Context, _ *metricdata.ResourceMetrics) error { return nil }
func (noopMetricExporter) ForceFlush(_ context.Context) error                            { return nil }
func (noopMetricExporter) Shutdown(_ context.Context) error                              { return nil }

type noopLogExporter struct{}

func (noopLogExporter) Export(_ context.Context, _ []sdklog.Record) error { return nil }
func (noopLogExporter) ForceFlush(_ context.Context) error                { return nil }
func (noopLogExporter) Shutdown(_ context.Context) error                  { return nil }

func TestBuildSampler_AlwaysOn(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "AlwaysOnSampler", buildSampler(1.0).Description())
	testza.AssertEqual(t, "AlwaysOnSampler", buildSampler(1.5).Description())
}

func TestBuildSampler_AlwaysOff(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "AlwaysOffSampler", buildSampler(0.0).Description())
	testza.AssertEqual(t, "AlwaysOffSampler", buildSampler(-0.5).Description())
}

func TestBuildSampler_Ratio(t *testing.T) {
	t.Parallel()
	testza.AssertTrue(t, strings.HasPrefix(buildSampler(0.5).Description(), "ParentBased{"))
}

func TestServiceInstanceID(t *testing.T) {
	t.Parallel()
	hostname, _ := os.Hostname()
	testza.AssertEqual(t, fmt.Sprintf("%s-%d", hostname, os.Getpid()), serviceInstanceID())
}

func TestBuildInfoAttrs_NoVersionCfg(t *testing.T) {
	t.Parallel()
	_ = buildInfoAttrs("") // must not panic; may be nil without embedded VCS info
}

func TestBuildInfoAttrs_CfgVersionSuppressesModuleVersion(t *testing.T) {
	t.Parallel()
	for _, kv := range buildInfoAttrs("v1.0.0") {
		testza.AssertNotEqual(t, attribute.Key("service.version"), kv.Key)
	}
}

func TestBuildResource_ServiceName(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.ServiceName = "my-svc"
	res, err := buildResource(context.Background(), cfg)
	testza.AssertNil(t, err)
	v, ok := res.Set().Value(attribute.Key("service.name"))
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "my-svc", v.AsString())
}

func TestBuildResource_OptionalFields(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.ServiceVersion = "1.2.3"
	cfg.ServiceNamespace = "myns"
	cfg.Environment = "staging"
	res, err := buildResource(context.Background(), cfg)
	testza.AssertNil(t, err)
	for key, want := range map[string]string{
		"service.version":             "1.2.3",
		"service.namespace":           "myns",
		"deployment.environment.name": "staging",
	} {
		v, ok := res.Set().Value(attribute.Key(key))
		testza.AssertTrue(t, ok)
		testza.AssertEqual(t, want, v.AsString())
	}
}

func TestNewTracerProvider(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.TraceExporter = tracetest.NewInMemoryExporter()
	res, err := buildResource(context.Background(), cfg)
	testza.AssertNil(t, err)
	tp, err := newTracerProvider(context.Background(), cfg, res)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, tp)
	testza.AssertNil(t, tp.Shutdown(context.Background()))
}

func TestNewMeterProvider(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.MetricExporter = noopMetricExporter{}
	res, err := buildResource(context.Background(), cfg)
	testza.AssertNil(t, err)
	mp, err := newMeterProvider(context.Background(), cfg, res)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, mp)
	testza.AssertNil(t, mp.Shutdown(context.Background()))
}

func TestNewLoggerProvider(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.LogExporter = noopLogExporter{}
	res, err := buildResource(context.Background(), cfg)
	testza.AssertNil(t, err)
	lp, err := newLoggerProvider(context.Background(), cfg, res)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, lp)
	testza.AssertNil(t, lp.Shutdown(context.Background()))
}

func TestWithTraceExporter(t *testing.T) {
	t.Parallel()
	exp := tracetest.NewInMemoryExporter()
	testza.AssertEqual(t, exp, NewConfig(WithTraceExporter(exp)).TraceExporter)
}

func TestWithMetricExporter(t *testing.T) {
	t.Parallel()
	testza.AssertNotNil(t, NewConfig(WithMetricExporter(noopMetricExporter{})).MetricExporter)
}

func TestWithLogExporter(t *testing.T) {
	t.Parallel()
	testza.AssertNotNil(t, NewConfig(WithLogExporter(noopLogExporter{})).LogExporter)
}

func TestWithPropagators(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, 1, len(NewConfig(WithPropagators(propagation.TraceContext{})).Propagators))
}

func TestNewTraceExporter_gRPC(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.Endpoint = "localhost:59999"
	cfg.Insecure = true
	exp, err := newTraceExporter(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, exp)
	testza.AssertNil(t, exp.Shutdown(context.Background()))
}

func TestNewTraceExporter_HTTP(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.Protocol = protocolHTTPProtobuf
	cfg.Endpoint = "localhost:59999"
	cfg.Insecure = true
	exp, err := newTraceExporter(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, exp)
	testza.AssertNil(t, exp.Shutdown(context.Background()))
}

func TestNewMetricExporter_gRPC(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.Endpoint = "localhost:59999"
	cfg.Insecure = true
	exp, err := newMetricExporter(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, exp)
	testza.AssertNil(t, exp.Shutdown(context.Background()))
}

func TestNewMetricExporter_HTTP(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.Protocol = protocolHTTPProtobuf
	cfg.Endpoint = "localhost:59999"
	cfg.Insecure = true
	exp, err := newMetricExporter(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, exp)
	testza.AssertNil(t, exp.Shutdown(context.Background()))
}

func TestNewLogExporter_gRPC(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.Endpoint = "localhost:59999"
	cfg.Insecure = true
	exp, err := newLogExporter(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, exp)
	testza.AssertNil(t, exp.Shutdown(context.Background()))
}

func TestNewLogExporter_HTTP(t *testing.T) {
	t.Parallel()
	cfg := NewDefaultConfig()
	cfg.Protocol = protocolHTTPProtobuf
	cfg.Endpoint = "localhost:59999"
	cfg.Insecure = true
	exp, err := newLogExporter(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, exp)
	testza.AssertNil(t, exp.Shutdown(context.Background()))
}

func TestSetupOTelSDK_MetricsOnly(t *testing.T) {
	// No t.Parallel() — mutates global OTel state, calls runtime.Start().
	cfg := NewDefaultConfig()
	cfg.Signals = []string{signalMetrics}
	cfg.MetricExporter = noopMetricExporter{}
	providers, err := setupOTelSDK(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNil(t, providers.tracerProvider)
	testza.AssertNotNil(t, providers.meterProvider)
	testza.AssertNil(t, providers.loggerProvider)
	testza.AssertNil(t, providers.shutdown(context.Background()))
}

// setupOTelSDK tests run sequentially — they mutate global OTel state.

func TestSetupOTelSDK_TraceOnly(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.Signals = []string{signalTraces}
	cfg.TraceExporter = tracetest.NewInMemoryExporter()
	providers, err := setupOTelSDK(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, providers.tracerProvider)
	testza.AssertNil(t, providers.meterProvider)
	testza.AssertNil(t, providers.loggerProvider)
	testza.AssertNil(t, providers.shutdown(context.Background()))
}

func TestSetupOTelSDK_LogOnly(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.Signals = []string{signalLogs}
	cfg.LogExporter = noopLogExporter{}
	providers, err := setupOTelSDK(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNil(t, providers.tracerProvider)
	testza.AssertNil(t, providers.meterProvider)
	testza.AssertNotNil(t, providers.loggerProvider)
	testza.AssertNil(t, providers.shutdown(context.Background()))
}

func TestSetupOTelSDK_NoSignals(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.Signals = []string{}
	providers, err := setupOTelSDK(context.Background(), cfg)
	testza.AssertNil(t, err)
	testza.AssertNil(t, providers.tracerProvider)
	testza.AssertNil(t, providers.meterProvider)
	testza.AssertNil(t, providers.loggerProvider)
	testza.AssertNil(t, providers.shutdown(context.Background()))
}
