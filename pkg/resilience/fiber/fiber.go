// Package fiber adapts named resilience policies into fiber v3 middleware.
// Import it aliased (e.g. resfiber) to avoid clashing with gofiber/fiber.
//
// Retry policies re-run the handler chain, which is rarely safe server-side;
// prefer rate_limit, circuit_breaker, and timeout policies in middleware.
package fiber

import (
	"errors"

	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/failsafe-go/failsafe-go/timeout"
	"github.com/gofiber/fiber/v3"
	"github.com/samber/oops"
)

// New runs the handler chain through the named policy, mapping policy
// rejections to HTTP status codes: rate limit to 429, open circuit breaker
// to 503, timeout to 504.
func New(reg *policy.Registry, name string) (fiber.Handler, error) {
	ex, err := reg.Executor(name)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to resolve resilience policy")
	}

	return func(c fiber.Ctx) error {
		err := ex.WithContext(c.Context()).RunWithExecution(func(_ failsafe.Execution[any]) error {
			return c.Next()
		})

		switch {
		case errors.Is(err, ratelimiter.ErrExceeded):
			return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
		case errors.Is(err, circuitbreaker.ErrOpen):
			return fiber.NewError(fiber.StatusServiceUnavailable, "circuit breaker open")
		case errors.Is(err, timeout.ErrExceeded):
			return fiber.NewError(fiber.StatusGatewayTimeout, "request timed out")
		default:
			return err //nolint:wrapcheck // handler errors pass through for fiber's error handler
		}
	}, nil
}
