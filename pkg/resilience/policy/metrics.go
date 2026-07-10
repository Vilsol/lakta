package policy

import (
	"context"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/Vilsol/lakta/pkg/resilience/policy"

// policyMetrics holds the shed/hedge counters and the meter used to register
// the per-limiter observable gauges. It no-ops when no MeterProvider is
// configured, since the OTel API yields usable no-op instruments.
type policyMetrics struct {
	meter metric.Meter
	shed  metric.Int64Counter // resilience.shed.total{policy,primitive}
	hedge metric.Int64Counter // resilience.hedge.total{policy}
}

// newPolicyMetrics reads otel.GetMeterProvider(); on error the OTel API yields
// usable no-op instruments, so the error is ignored (matches query_metrics.go).
func newPolicyMetrics() *policyMetrics {
	meter := otel.GetMeterProvider().Meter(meterName)
	shed, _ := meter.Int64Counter(
		"resilience.shed.total",
		metric.WithDescription("Executions shed by a resilience load-shedding primitive."),
	)
	hedge, _ := meter.Int64Counter(
		"resilience.hedge.total",
		metric.WithDescription("Hedged execution attempts started by a resilience policy."),
	)
	return &policyMetrics{meter: meter, shed: shed, hedge: hedge}
}

// onShed returns a listener that increments resilience.shed.total for
// (policy, primitive). Returns nil when pm is nil so builders skip wiring.
func (pm *policyMetrics) onShed(policy, primitive string) func(failsafe.ExecutionEvent[any]) {
	if pm == nil {
		return nil
	}
	attrs := metric.WithAttributes(
		attribute.String("policy", policy),
		attribute.String("primitive", primitive),
	)
	return func(event failsafe.ExecutionEvent[any]) {
		pm.shed.Add(event.Context(), 1, attrs)
	}
}

// onHedge returns a listener that increments resilience.hedge.total for policy.
// Returns nil when pm is nil so builders skip wiring.
func (pm *policyMetrics) onHedge(policy string) func(failsafe.ExecutionEvent[any]) {
	if pm == nil {
		return nil
	}
	attrs := metric.WithAttributes(attribute.String("policy", policy))
	return func(event failsafe.ExecutionEvent[any]) {
		pm.hedge.Add(event.Context(), 1, attrs)
	}
}

// registerLimiterGauges registers resilience.limit{policy} and
// resilience.inflight{policy} as Int64 observable gauges reading m.Limit() and
// m.Inflight(). Called once per adaptive limiter during Init.
func (pm *policyMetrics) registerLimiterGauges(policy string, m adaptivelimiter.Metrics) error {
	attrs := metric.WithAttributes(attribute.String("policy", policy))

	if _, err := pm.meter.Int64ObservableGauge(
		"resilience.limit",
		metric.WithDescription("Current concurrency limit of an adaptive limiter."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(m.Limit()), attrs)
			return nil
		}),
	); err != nil {
		return err //nolint:wrapcheck // otel registration error surfaced to the caller for wrapping
	}

	if _, err := pm.meter.Int64ObservableGauge(
		"resilience.inflight",
		metric.WithDescription("Current inflight executions tracked by an adaptive limiter."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(m.Inflight()), attrs)
			return nil
		}),
	); err != nil {
		return err //nolint:wrapcheck // otel registration error surfaced to the caller for wrapping
	}

	return nil
}
