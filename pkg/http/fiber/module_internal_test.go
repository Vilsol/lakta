package fiberserver

import (
	"context"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/gofiber/fiber/v3"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

func injectRuntimeCtx(m *Module, ctx context.Context) {
	rv := reflect.ValueOf(&m.SyncCtx).Elem()
	f := rv.FieldByName("ctx")
	reflect.NewAt(f.Type(), f.Addr().UnsafePointer()).Elem().Set(reflect.ValueOf(ctx))
}

func TestStart_DoesNotStopServerOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := NewModule(
		WithHost("127.0.0.1"),
		WithPort(0),
		WithRouter(func(app *fiber.App) {
			app.Get("/ping", func(c fiber.Ctx) error {
				return c.SendStatus(http.StatusOK)
			})
		}),
	)
	injectRuntimeCtx(m, ctx)
	testza.AssertNoError(t, m.Init(ctx))

	startCtx, cancelStart := context.WithCancel(ctx)
	startErr := make(chan error, 1)
	go func() {
		startErr <- m.Start(startCtx)
	}()

	// Poll until server is listening.
	var addr net.Addr
	for i := range 50 {
		addr = m.Addr()
		if addr != nil {
			break
		}
		if i == 49 {
			t.Fatal("server did not start listening within timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancelStart()

	select {
	case err := <-startErr:
		testza.AssertNil(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}

	// Server must still be serving — a fresh GET /ping must succeed.
	reqCtx, reqCancel := context.WithTimeout(ctx, 2*time.Second)
	defer reqCancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr.String()+"/ping", nil)
	testza.AssertNoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	testza.AssertNoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
		testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
	}

	testza.AssertNoError(t, m.Shutdown(context.Background()))
}

func TestToFiberConfig_AppliesGenerousTimeoutDefaults(t *testing.T) {
	t.Parallel()

	c := NewConfig()
	cfg := c.ToFiberConfig()

	testza.AssertEqual(t, 30*time.Second, cfg.ReadTimeout)
	testza.AssertEqual(t, 60*time.Second, cfg.WriteTimeout)
	testza.AssertEqual(t, 120*time.Second, cfg.IdleTimeout)
}

func TestToFiberConfig_UserDefaultsOverrideTimeouts(t *testing.T) {
	t.Parallel()

	c := NewConfig(WithDefaults(fiber.Config{ReadTimeout: 5 * time.Second}))
	cfg := c.ToFiberConfig()

	testza.AssertEqual(t, 5*time.Second, cfg.ReadTimeout)   // user value preserved
	testza.AssertEqual(t, 60*time.Second, cfg.WriteTimeout) // unset → default
}
