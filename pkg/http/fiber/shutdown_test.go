package fiberserver_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
)

func TestFiberModule_ShutdownDrainsAndClosesListener(t *testing.T) {
	t.Parallel()

	reached := make(chan struct{}, 1)
	released := make(chan struct{})

	m := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(0),
		fiberserver.WithRouter(func(app *fiber.App) {
			app.Get("/slow", func(c fiber.Ctx) error {
				reached <- struct{}{}
				<-released
				return c.SendString("done")
			})
		}),
	)

	h := testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)

	// Fire an in-flight slow request that blocks in the handler until released.
	type respPair struct {
		status int
		err    error
	}
	inflight := make(chan respPair, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr.String()+"/slow", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			inflight <- respPair{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		inflight <- respPair{status: resp.StatusCode}
	}()

	// Wait until the handler is actually executing before shutting down.
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not reach handler")
	}

	// Trigger graceful shutdown in the background; a draining shutdown must wait
	// for the in-flight request rather than dropping it.
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- h.Shutdown() }()

	// Release the in-flight request so it can complete during drain.
	close(released)

	got := <-inflight
	testza.AssertNil(t, got.err)
	testza.AssertEqual(t, http.StatusOK, got.status)

	select {
	case err := <-shutdownDone:
		testza.AssertNil(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete")
	}

	// Listener must be closed: new connections refused.
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer dialCancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr.String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected connection to be refused after shutdown")
	}
}
