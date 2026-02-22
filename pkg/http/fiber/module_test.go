package fiberserver_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/health"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
)

func TestFiberModule_Listens(t *testing.T) {
	t.Parallel()

	m := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(0),
		fiberserver.WithRouter(func(app *fiber.App) {
			app.Get("/ping", func(c fiber.Ctx) error {
				return c.SendString("pong")
			})
		}),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)
	testza.AssertNotNil(t, addr)
	testza.AssertNotEqual(t, "", addr.String())
}

func TestFiberModule_ServesRoute(t *testing.T) {
	t.Parallel()

	m := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(0),
		fiberserver.WithRouter(func(app *fiber.App) {
			app.Get("/ping", func(c fiber.Ctx) error {
				return c.SendString("pong")
			})
		}),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)

	resp, err := http.Get("http://" + addr.String() + "/ping") //nolint:noctx
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "pong", string(body))
}

func TestFiberModule_HealthPath(t *testing.T) {
	t.Parallel()

	healthM := health.NewModule(health.WithComponentName("test"))
	fiberM := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(0),
		fiberserver.WithHealthPath("/health"),
	)

	// health must init before fiber so *health.Health is in DI when fiber.Start runs
	testkit.NewRuntimeHarness(t, healthM, fiberM)

	addr := testkit.WaitForAddr(t, fiberM)

	resp, err := http.Get("http://" + addr.String() + "/health") //nolint:noctx
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
}

func TestFiberModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.http.fiber.default", fiberserver.NewModule().ConfigPath())
}

func TestFiberModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", fiberserver.NewModule().Name())
	testza.AssertEqual(t, "custom", fiberserver.NewModule(fiberserver.WithName("custom")).Name())
}

func TestFiberModule_AddrNilBeforeStart(t *testing.T) {
	t.Parallel()

	m := fiberserver.NewModule()
	testza.AssertNil(t, m.Addr())
}
