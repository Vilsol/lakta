package fiber_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/gofiber/fiber/v3"
)

func TestNew_BulkheadOverloadMapsTo503(t *testing.T) {
	t.Parallel()

	bh := bulkhead.NewBuilder[any](1).Build()
	testza.AssertNoError(t, bh.AcquirePermit(context.Background())) // saturate the only slot

	reg := newRegistry(t, policy.WithPolicy("api", bh))
	app := newApp(t, reg, "api")

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, fiber.StatusServiceUnavailable, resp.StatusCode)
	testza.AssertEqual(t, "1", resp.Header.Get(fiber.HeaderRetryAfter))
}

func TestNew_AdaptiveLimiterOverloadMapsTo503(t *testing.T) {
	t.Parallel()

	limiter := adaptivelimiter.NewBuilder[any]().WithLimits(1, 1, 1).Build()
	_, ok := limiter.TryAcquirePermit() // saturate the single permit
	testza.AssertTrue(t, ok)

	reg := newRegistry(t, policy.WithPolicy("api", limiter))
	app := newApp(t, reg, "api")

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, fiber.StatusServiceUnavailable, resp.StatusCode)
	testza.AssertEqual(t, "1", resp.Header.Get(fiber.HeaderRetryAfter))
}
