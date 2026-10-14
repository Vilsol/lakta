package connect

import (
	"context"
	"maps"

	"connectrpc.com/connect"
	"github.com/samber/oops"
)

// Slot names a position in the server interceptor chain. Slots run outer to
// inner in declaration order, so an error raised in any slot below
// SlotErrorBoundary is rendered, logged and recorded once by the boundary.
type Slot int

// Interceptor slots, outer to inner.
const (
	SlotOtel Slot = iota
	SlotRequestID
	SlotAccessLog
	SlotErrorBoundary
	SlotRecovery
	SlotDeadline
	SlotAuth
	SlotAuthz
	SlotValidate
	SlotUser

	slotCount
)

// InterceptorFactory builds a slot's interceptor during Init, when the injector
// is in ctx, so interceptors can resolve DI dependencies. A nil interceptor
// leaves the slot empty; an error fails Init.
type InterceptorFactory func(ctx context.Context) (connect.Interceptor, error)

// WithSlot sets the factory for a slot, replacing the built-in one if any.
// Security interceptors (SlotAuth, SlotAuthz) must implement both WrapUnary and
// WrapStreamingHandler; a connect.UnaryInterceptorFunc would leave streams open.
func WithSlot(slot Slot, factory InterceptorFactory) Option {
	return func(m *Config) {
		if m.Slots == nil {
			m.Slots = make(map[Slot]InterceptorFactory)
		}
		m.Slots[slot] = factory
	}
}

func (c *Config) defaultSlots() map[Slot]InterceptorFactory {
	static := func(ic connect.Interceptor) InterceptorFactory {
		return func(context.Context) (connect.Interceptor, error) { return ic, nil }
	}

	return map[Slot]InterceptorFactory{
		SlotOtel:          func(context.Context) (connect.Interceptor, error) { return otelInterceptor(), nil },
		SlotRequestID:     static(requestIDInterceptor{}),
		SlotAccessLog:     static(accessLogInterceptor{}),
		SlotErrorBoundary: static(errorBoundaryInterceptor{}),
		SlotRecovery:      static(recoveryInterceptor{}),
		SlotDeadline:      static(deadlineInterceptor{defaultDeadline: c.DefaultDeadline, maxDeadline: c.MaxDeadline}),
		SlotValidate:      static(newValidationInterceptor()),
	}
}

// Interceptors builds the slot chain (outer to inner) plus the read limit as
// the []connect.HandlerOption every WithService registrar receives.
func (c *Config) Interceptors(ctx context.Context) ([]connect.HandlerOption, error) {
	factories := c.defaultSlots()
	maps.Copy(factories, c.Slots)

	ics := make([]connect.Interceptor, 0, int(slotCount)+len(c.ExtraInterceptors))
	for slot := range slotCount {
		f, ok := factories[slot]
		if !ok || f == nil {
			continue
		}
		ic, err := f(ctx)
		if err != nil {
			return nil, oops.With("slot", int(slot)).Wrapf(err, "failed to build interceptor")
		}
		if ic != nil {
			ics = append(ics, ic)
		}
	}
	ics = append(ics, c.ExtraInterceptors...)

	opts := []connect.HandlerOption{connect.WithInterceptors(ics...)}
	if c.ReadMaxBytes > 0 {
		opts = append(opts, connect.WithReadMaxBytes(c.ReadMaxBytes))
	}

	return opts, nil
}
