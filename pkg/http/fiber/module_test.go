package fiberserver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/health"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
)

func TestFiberModule_StartBindFailure(t *testing.T) {
	t.Parallel()

	// Occupy a port, then point the server at it so Listen fails.
	occupied, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = occupied.Close() })
	tcpAddr, ok := occupied.Addr().(*net.TCPAddr)
	testza.AssertTrue(t, ok)
	port := uint16(tcpAddr.Port) //nolint:gosec // OS-assigned ephemeral port fits uint16

	m := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(port),
	)

	rh := testkit.NewRuntimeHarness(t, m)
	startErr := rh.Shutdown()

	testza.AssertNotNil(t, startErr)
	testza.AssertContains(t, startErr.Error(), "failed to listen")
}

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

type ctxGreeting struct{ msg string }

func TestFiberModule_RouterCtxResolvesDI(t *testing.T) {
	t.Parallel()

	// A plain module that inits before fiber (original order, no declared deps)
	// and seeds a value into DI for the RouterCtx closure to resolve.
	provider := testkit.NewMockModule()
	provider.OnInit = func(ctx context.Context) error {
		lakta.ProvideValue(ctx, &ctxGreeting{msg: "hello-di"})
		return nil
	}

	var gotNilCtx bool

	m := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(0),
		fiberserver.WithRouter(func(app *fiber.App) {
			app.Get("/plain", func(c fiber.Ctx) error {
				return c.SendString("plain")
			})
		}),
		fiberserver.WithRouterCtx(func(ctx context.Context, app *fiber.App) {
			gotNilCtx = ctx == nil
			app.Get("/ctx", func(c fiber.Ctx) error {
				g, err := lakta.Invoke[*ctxGreeting](ctx)
				if err != nil {
					return c.Status(http.StatusInternalServerError).SendString(err.Error())
				}
				return c.SendString(g.msg)
			})
		}),
	)

	testkit.NewRuntimeHarness(t, provider, m)

	addr := testkit.WaitForAddr(t, m)
	testza.AssertFalse(t, gotNilCtx, "RouterCtx must receive a non-nil ctx at Init")

	for path, want := range map[string]string{"/plain": "plain", "/ctx": "hello-di"} {
		resp, err := http.Get("http://" + addr.String() + path) //nolint:noctx
		testza.AssertNil(t, err)

		body, err := io.ReadAll(resp.Body)
		testza.AssertNil(t, err)
		_ = resp.Body.Close()

		testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
		testza.AssertEqual(t, want, string(body))
	}
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
