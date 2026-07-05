package actuator

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
)

func passAuth(c fiber.Ctx) error { return c.Next() } //nolint:wrapcheck

// initModule builds and initializes an actuator with the given options against a
// fresh harness, returning the module and the Init error for inspection.
func initModule(t *testing.T, opts ...Option) (*Module, error) {
	t.Helper()
	h := testkit.NewHarness(t)
	act := NewModule(opts...)
	return act, act.Init(h.Ctx())
}

func TestSecurityNonLoopbackNoAuthRefuses(t *testing.T) {
	t.Parallel()

	_, err := initModule(t, WithEnabled(true), WithHost("0.0.0.0"))
	testza.AssertNotNil(t, err)
}

func TestSecurityNonLoopbackAllowInsecureStarts(t *testing.T) {
	t.Parallel()

	act := NewModule(WithEnabled(true), WithHost("0.0.0.0"))
	act.config.AllowInsecure = true

	h := testkit.NewHarness(t)
	err := act.Init(h.Ctx())
	testza.AssertNoError(t, err)
}

func TestSecuritySensitiveEndpointsRejectWithoutAuth(t *testing.T) {
	t.Parallel()

	act, err := initModule(t, WithEnabled(true))
	testza.AssertNoError(t, err)

	for _, path := range []struct {
		method string
		url    string
	}{
		{http.MethodGet, "/debug/goroutine"},
		{http.MethodGet, "/debug/vars"},
		{http.MethodGet, "/debug/pprof/"},
		{http.MethodPost, "/debug/loggers"},
	} {
		req := httptest.NewRequestWithContext(t.Context(), path.method, path.url, nil)
		resp, testErr := act.app.Test(req)
		testza.AssertNoError(t, testErr)
		testza.AssertEqual(t, http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestSecuritySensitiveEndpointsAllowWithAuth(t *testing.T) {
	t.Parallel()

	act, err := initModule(t, WithEnabled(true), WithAuth(passAuth))
	testza.AssertNoError(t, err)

	for _, url := range []string{"/debug/goroutine", "/debug/vars"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		resp, testErr := act.app.Test(req)
		testza.AssertNoError(t, testErr)
		testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
	}
}

func TestSecurityLoggersUnconditionalEvenAllowInsecure(t *testing.T) {
	t.Parallel()

	act := NewModule(WithEnabled(true))
	act.config.AllowInsecure = true

	h := testkit.NewHarness(t)
	testza.AssertNoError(t, act.Init(h.Ctx()))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/debug/loggers", nil)
	resp, testErr := act.app.Test(req)
	testza.AssertNoError(t, testErr)
	testza.AssertEqual(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSecurityPprofSecondsClamped(t *testing.T) {
	t.Parallel()

	act, err := initModule(t, WithEnabled(true), WithAuth(passAuth))
	testza.AssertNoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/pprof/profile?seconds=9999", nil)
	resp, testErr := act.app.Test(req)
	testza.AssertNoError(t, testErr)
	testza.AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
}
