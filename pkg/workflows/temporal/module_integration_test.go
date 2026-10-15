//go:build integration

package temporal_test

import (
	"context"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	pkgtemporal "github.com/Vilsol/lakta/pkg/workflows/temporal"
	"go.temporal.io/sdk/testsuite"
)

func startDevServer(t *testing.T) *testsuite.DevServer {
	t.Helper()

	srv, err := testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{
		CachedDownload: testsuite.CachedDownload{Version: "default"},
		LogLevel:       "error",
	})
	if err != nil {
		t.Skipf("temporal dev server unavailable: %v", err)
	}

	t.Cleanup(func() { _ = srv.Stop() })

	return srv
}

func TestTemporalModule_Start_ConnectsAndRunsWorker(t *testing.T) {
	t.Parallel()

	srv := startDevServer(t)
	m := pkgtemporal.NewModule(
		pkgtemporal.WithTarget(srv.FrontendHostPort()),
		pkgtemporal.WithInsecure(true),
		pkgtemporal.WithTaskQueue("test"),
	)

	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())
}

func TestTemporalModule_ReadyOnlyWithExistingNamespace(t *testing.T) {
	t.Parallel()

	srv := startDevServer(t)

	ready := pkgtemporal.NewModule(
		pkgtemporal.WithTarget(srv.FrontendHostPort()),
		pkgtemporal.WithTaskQueue("ready-queue"),
		pkgtemporal.WithInsecure(true),
	)
	missing := pkgtemporal.NewModule(
		pkgtemporal.WithName("missing"),
		pkgtemporal.WithTarget(srv.FrontendHostPort()),
		pkgtemporal.WithNamespace("does-not-exist"),
		pkgtemporal.WithTaskQueue("missing-queue"),
		pkgtemporal.WithInsecure(true),
	)
	testkit.NewRuntimeHarness(t, ready)
	testkit.NewRuntimeHarness(t, missing)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && ready.Ready(t.Context()) != nil {
		time.Sleep(100 * time.Millisecond)
	}
	testza.AssertNil(t, ready.Ready(t.Context()))
	testza.AssertNotNil(t, missing.Ready(t.Context()))
}
