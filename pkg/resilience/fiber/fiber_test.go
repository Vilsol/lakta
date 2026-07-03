package fiber_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	resfiber "github.com/Vilsol/lakta/pkg/resilience/fiber"
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"github.com/gofiber/fiber/v3"
	"github.com/samber/do/v2"
)

func newRegistry(t *testing.T, options ...policy.Option) *policy.Registry {
	t.Helper()
	h := testkit.NewHarness(t)
	m := policy.NewModule(options...)
	testza.AssertNoError(t, m.Init(h.Ctx()))

	reg, err := do.Invoke[*policy.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	return reg
}

func newApp(t *testing.T, reg *policy.Registry, policyName string) *fiber.App {
	t.Helper()
	middleware, err := resfiber.New(reg, policyName)
	testza.AssertNoError(t, err)

	app := fiber.New()
	app.Use(middleware)
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func TestNew_PassesThroughWithinLimits(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t, policy.WithPolicy("api",
		ratelimiter.NewBursty[any](10, time.Minute),
	))
	app := newApp(t, reg, "api")

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, fiber.StatusOK, resp.StatusCode)
}

func TestNew_RateLimitMapsTo429(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t, policy.WithPolicy("api",
		ratelimiter.NewBursty[any](1, time.Minute),
	))
	app := newApp(t, reg, "api")

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, fiber.StatusOK, resp.StatusCode)

	resp, err = app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, fiber.StatusTooManyRequests, resp.StatusCode)
}

func TestNew_OpenBreakerMapsTo503(t *testing.T) {
	t.Parallel()
	breaker := circuitbreaker.NewBuilder[any]().WithFailureThreshold(1).Build()
	breaker.Open()
	reg := newRegistry(t, policy.WithPolicy("api", breaker))
	app := newApp(t, reg, "api")

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	testza.AssertNoError(t, err)
	testza.AssertEqual(t, fiber.StatusServiceUnavailable, resp.StatusCode)
}

func TestNew_UnknownPolicyErrors(t *testing.T) {
	t.Parallel()
	reg := newRegistry(t)

	_, err := resfiber.New(reg, "missing")
	testza.AssertNotNil(t, err)
}
