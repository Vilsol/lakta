package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"

	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.36.0"
)

type otelProviders struct {
	shutdown       func(context.Context) error
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *sdklog.LoggerProvider
}

func setupOTelSDK(ctx context.Context, cfg Config) (otelProviders, error) {
	var shutdownFuncs []func(context.Context) error

	shutdownAll := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(handlerErr error) {
		slog.Default().Error("OpenTelemetry internal error", slog.Any("error", handlerErr))
	}))

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(cfg.Propagators...))

	res, err := buildResource(ctx, cfg)
	if err != nil {
		_ = shutdownAll(ctx)
		return otelProviders{}, err
	}

	var providers otelProviders
	providers.shutdown = shutdownAll

	if slices.Contains(cfg.Signals, signalTraces) {
		tp, tpErr := newTracerProvider(ctx, cfg, res)
		if tpErr != nil {
			_ = shutdownAll(ctx)
			return otelProviders{}, tpErr
		}
		shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
		otel.SetTracerProvider(tp)
		providers.tracerProvider = tp
	}

	if slices.Contains(cfg.Signals, signalMetrics) {
		mp, mpErr := newMeterProvider(ctx, cfg, res)
		if mpErr != nil {
			_ = shutdownAll(ctx)
			return otelProviders{}, mpErr
		}
		shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
		otel.SetMeterProvider(mp)
		providers.meterProvider = mp

		// runtime metrics goroutines stop naturally when the meterProvider shuts down
		if rErr := runtime.Start(runtime.WithMinimumReadMemStatsInterval(cfg.RuntimeInterval)); rErr != nil {
			_ = shutdownAll(ctx)
			return otelProviders{}, oops.Wrapf(rErr, "failed to start runtime metrics")
		}
	}

	if slices.Contains(cfg.Signals, signalLogs) {
		lp, lpErr := newLoggerProvider(ctx, cfg, res)
		if lpErr != nil {
			_ = shutdownAll(ctx)
			return otelProviders{}, lpErr
		}
		shutdownFuncs = append(shutdownFuncs, lp.Shutdown)
		global.SetLoggerProvider(lp)
		providers.loggerProvider = lp
	}

	return providers, nil
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.ServiceName),
		semconv.ServiceInstanceIDKey.String(serviceInstanceID()),
	}
	attrs = append(attrs, buildInfoAttrs(cfg.ServiceVersion)...)
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(cfg.ServiceVersion))
	}
	if cfg.ServiceNamespace != "" {
		attrs = append(attrs, semconv.ServiceNamespaceKey.String(cfg.ServiceNamespace))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(cfg.Environment))
	}

	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
	return res, oops.Wrapf(err, "failed to create OTEL resource")
}

// buildInfoAttrs reads VCS and module version from the embedded Go build info.
// cfgVersion is the explicitly configured service version; when non-empty, the
// module version fallback is skipped so user config always wins.
func buildInfoAttrs(cfgVersion string) []attribute.KeyValue {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}

	var attrs []attribute.KeyValue

	if cfgVersion == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(bi.Main.Version))
	}

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			attrs = append(attrs, attribute.String("vcs.revision", s.Value))
		case "vcs.time":
			attrs = append(attrs, attribute.String("vcs.time", s.Value))
		case "vcs.modified":
			attrs = append(attrs, attribute.String("vcs.modified", s.Value))
		}
	}

	return attrs
}

func serviceInstanceID() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func newTracerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	exp, err := newTraceExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(buildSampler(cfg.SampleRate)),
	), nil
}

//nolint:dupl,ireturn
func newTraceExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	if cfg.TraceExporter != nil {
		return cfg.TraceExporter, nil
	}
	switch cfg.Protocol {
	case protocolHTTPProtobuf, protocolHTTPJSON:
		var opts []otlptracehttp.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		return exp, oops.Wrapf(err, "failed to create OTLP HTTP trace exporter")
	default: // grpc
		var opts []otlptracegrpc.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		exp, err := otlptracegrpc.New(ctx, opts...)
		return exp, oops.Wrapf(err, "failed to create OTLP gRPC trace exporter")
	}
}

func newMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	exp, err := newMetricExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(cfg.MetricInterval))

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
		sdkmetric.WithExemplarFilter(exemplar.TraceBasedFilter),
	), nil
}

//nolint:dupl,ireturn
func newMetricExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	if cfg.MetricExporter != nil {
		return cfg.MetricExporter, nil
	}
	switch cfg.Protocol {
	case protocolHTTPProtobuf, protocolHTTPJSON:
		var opts []otlpmetrichttp.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetrichttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
		}
		exp, err := otlpmetrichttp.New(ctx, opts...)
		return exp, oops.Wrapf(err, "failed to create OTLP HTTP metric exporter")
	default: // grpc
		var opts []otlpmetricgrpc.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
		}
		exp, err := otlpmetricgrpc.New(ctx, opts...)
		return exp, oops.Wrapf(err, "failed to create OTLP gRPC metric exporter")
	}
}

func newLoggerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	exp, err := newLogExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
		sdklog.WithResource(res),
	), nil
}

//nolint:dupl,ireturn
func newLogExporter(ctx context.Context, cfg Config) (sdklog.Exporter, error) {
	if cfg.LogExporter != nil {
		return cfg.LogExporter, nil
	}
	switch cfg.Protocol {
	case protocolHTTPProtobuf, protocolHTTPJSON:
		var opts []otlploghttp.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlploghttp.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlploghttp.WithHeaders(cfg.Headers))
		}
		exp, err := otlploghttp.New(ctx, opts...)
		return exp, oops.Wrapf(err, "failed to create OTLP HTTP log exporter")
	default: // grpc
		var opts []otlploggrpc.Option
		if cfg.Endpoint != "" {
			opts = append(opts, otlploggrpc.WithEndpoint(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlploggrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlploggrpc.WithHeaders(cfg.Headers))
		}
		exp, err := otlploggrpc.New(ctx, opts...)
		return exp, oops.Wrapf(err, "failed to create OTLP gRPC log exporter")
	}
}

func buildSampler(rate float64) sdktrace.Sampler { //nolint:ireturn
	switch {
	case rate >= 1.0:
		return sdktrace.AlwaysSample()
	case rate <= 0.0:
		return sdktrace.NeverSample()
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate))
	}
}
