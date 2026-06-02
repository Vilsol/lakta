package temporal_test

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	pkgtemporal "github.com/Vilsol/lakta/pkg/workflows/temporal"
)

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
