package temporal_test

import (
	"context"
	"testing"

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

func TestTemporalModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.workflows.temporal.default", pkgtemporal.NewModule().ConfigPath())
}

func TestTemporalModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", pkgtemporal.NewModule().Name())
	testza.AssertEqual(t, "custom", pkgtemporal.NewModule(pkgtemporal.WithName("custom")).Name())
}

func TestTemporalModule_Init_RequiresTaskQueue(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	m := pkgtemporal.NewModule()
	testza.AssertNotNil(t, m.Init(h.Ctx()))
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
