package otel_test

import (
	"context"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/otel"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	otellog "go.opentelemetry.io/otel/log"
	otelmetric "go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestOtelModule_InitWithSetupFn(t *testing.T) {
	t.Parallel()

	var initCalls atomic.Int32
	setupFn := func(_ context.Context, _ string) (func(context.Context) error, error) {
		initCalls.Add(1)
		return func(_ context.Context) error { return nil }, nil
	}

	h := testkit.NewRuntimeHarness(t, otel.NewModule(otel.WithSetupFn(setupFn)))
	testza.AssertNil(t, h.Shutdown())
	testza.AssertEqual(t, int32(1), initCalls.Load())
}

func TestOtelModule_ShutdownCallsReturnedFn(t *testing.T) {
	t.Parallel()

	var shutdownCalls atomic.Int32
	setupFn := func(_ context.Context, _ string) (func(context.Context) error, error) {
		return func(_ context.Context) error {
			shutdownCalls.Add(1)
			return nil
		}, nil
	}

	h := testkit.NewRuntimeHarness(t, otel.NewModule(otel.WithSetupFn(setupFn)))
	testza.AssertNil(t, h.Shutdown())
	testza.AssertEqual(t, int32(1), shutdownCalls.Load())
}

func TestOtelModule_SetupFnReceivesServiceName(t *testing.T) {
	t.Parallel()

	var gotName string
	setupFn := func(_ context.Context, name string) (func(context.Context) error, error) {
		gotName = name
		return func(_ context.Context) error { return nil }, nil
	}

	h := testkit.NewRuntimeHarness(t, otel.NewModule(
		otel.WithSetupFn(setupFn),
		otel.WithServiceName("my-svc"),
	))
	testza.AssertNil(t, h.Shutdown())
	testza.AssertEqual(t, "my-svc", gotName)
}

func TestOtelModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.otel.otel.default", otel.NewModule().ConfigPath())
}

func TestOtelModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", otel.NewModule().Name())
	testza.AssertEqual(t, "custom", otel.NewModule(otel.WithName("custom")).Name())
}

func TestOtelModule_Disabled(t *testing.T) {
	t.Parallel()

	var setupCalled atomic.Int32
	setupFn := func(_ context.Context, _ string) (func(context.Context) error, error) {
		setupCalled.Add(1)
		return func(_ context.Context) error { return nil }, nil
	}

	h := testkit.NewRuntimeHarness(t, otel.NewModule(
		otel.WithEnabled(false),
		otel.WithSetupFn(setupFn),
	))
	testza.AssertNil(t, h.Shutdown())
	testza.AssertEqual(t, int32(0), setupCalled.Load())
}

func TestOtelModule_DisabledRegistersNoopProviders(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	m := otel.NewModule(otel.WithEnabled(false))
	testza.AssertNil(t, m.Init(h.Ctx()))

	tp, err := do.Invoke[oteltrace.TracerProvider](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, tp)

	mp, err := do.Invoke[otelmetric.MeterProvider](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, mp)

	lp, err := do.Invoke[otellog.LoggerProvider](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, lp)
}

func TestOtelModule_ProvidesTypes(t *testing.T) {
	t.Parallel()

	m := otel.NewModule()
	provides := m.Provides()

	testza.AssertEqual(t, 3, len(provides))
	testza.AssertTrue(t, containsType(provides, reflect.TypeFor[oteltrace.TracerProvider]()))
	testza.AssertTrue(t, containsType(provides, reflect.TypeFor[otelmetric.MeterProvider]()))
	testza.AssertTrue(t, containsType(provides, reflect.TypeFor[otellog.LoggerProvider]()))
}

func containsType(types []reflect.Type, target reflect.Type) bool {
	return slices.Contains(types, target)
}

func TestOtelModule_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := otel.NewDefaultConfig()
	testza.AssertEqual(t, "lakta", cfg.ServiceName)
	testza.AssertEqual(t, "grpc", cfg.Protocol)
	testza.AssertEqual(t, float64(1.0), cfg.SampleRate)
	testza.AssertEqual(t, 60*time.Second, cfg.MetricInterval)
	testza.AssertEqual(t, time.Second, cfg.RuntimeInterval)
	testza.AssertEqual(t, true, cfg.Enabled)
	testza.AssertEqual(t, []string{"traces", "metrics", "logs"}, cfg.Signals)
	testza.AssertEqual(t, 2, len(cfg.Propagators))
}

func TestOtelModule_WithOptions(t *testing.T) {
	t.Parallel()

	cfg := otel.NewConfig(
		otel.WithServiceName("svc"),
		otel.WithServiceVersion("1.2.3"),
		otel.WithServiceNamespace("payments"),
		otel.WithEnvironment("production"),
		otel.WithEndpoint("localhost:4317"),
		otel.WithProtocol("http/protobuf"),
		otel.WithInsecure(true),
		otel.WithHeaders(map[string]string{"x-token": "abc"}),
		otel.WithSampleRate(0.5),
		otel.WithMetricInterval(30*time.Second),
		otel.WithRuntimeInterval(2*time.Second),
		otel.WithEnabled(false),
		otel.WithSignals("traces"),
	)

	testza.AssertEqual(t, "svc", cfg.ServiceName)
	testza.AssertEqual(t, "1.2.3", cfg.ServiceVersion)
	testza.AssertEqual(t, "payments", cfg.ServiceNamespace)
	testza.AssertEqual(t, "production", cfg.Environment)
	testza.AssertEqual(t, "localhost:4317", cfg.Endpoint)
	testza.AssertEqual(t, "http/protobuf", cfg.Protocol)
	testza.AssertEqual(t, true, cfg.Insecure)
	testza.AssertEqual(t, "abc", cfg.Headers["x-token"])
	testza.AssertEqual(t, 0.5, cfg.SampleRate)
	testza.AssertEqual(t, 30*time.Second, cfg.MetricInterval)
	testza.AssertEqual(t, 2*time.Second, cfg.RuntimeInterval)
	testza.AssertEqual(t, false, cfg.Enabled)
	testza.AssertEqual(t, []string{"traces"}, cfg.Signals)
}

func TestOtelModule_KoanfLoad(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"modules": map[string]any{
			"otel": map[string]any{
				"otel": map[string]any{
					"default": map[string]any{
						"service_name":     "from-config",
						"service_version":  "2.0.0",
						"environment":      "staging",
						"endpoint":         "otelcol:4317",
						"insecure":         true,
						"sample_rate":      0.1,
						"metric_interval":  "30s",
						"runtime_interval": "5s",
						"signals":          []any{"traces", "logs"},
					},
				},
			},
		},
	})

	m := otel.NewModule()
	testza.AssertNil(t, m.LoadConfig(do.MustInvoke[*koanf.Koanf](h.Injector())))

	testza.AssertEqual(t, "from-config", m.Config().ServiceName)
	testza.AssertEqual(t, "2.0.0", m.Config().ServiceVersion)
	testza.AssertEqual(t, "staging", m.Config().Environment)
	testza.AssertEqual(t, "otelcol:4317", m.Config().Endpoint)
	testza.AssertEqual(t, true, m.Config().Insecure)
	testza.AssertEqual(t, 0.1, m.Config().SampleRate)
	testza.AssertEqual(t, 30*time.Second, m.Config().MetricInterval)
	testza.AssertEqual(t, 5*time.Second, m.Config().RuntimeInterval)
	testza.AssertEqual(t, []string{"traces", "logs"}, m.Config().Signals)
}
