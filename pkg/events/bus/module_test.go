package bus_test

import (
	"reflect"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/events/bus"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/samber/do/v2"
)

func TestModule_ProvidesBusInDI(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := bus.NewModule()

	testza.AssertNoError(t, m.Init(h.Ctx()))

	b, err := do.Invoke[*bus.Bus](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	testza.AssertNotNil(t, b)
}

func TestModule_ProvidesDeclaresBusType(t *testing.T) {
	t.Parallel()
	types := bus.NewModule().Provides()

	testza.AssertEqual(t, 1, len(types))
	testza.AssertTrue(t, types[0] == reflect.TypeFor[*bus.Bus]())
}

func TestModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.events.bus.default", bus.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.events.bus.custom", bus.NewModule(bus.WithName("custom")).ConfigPath())
}

func TestModule_ShutdownClosesBus(t *testing.T) {
	t.Parallel()
	h := testkit.NewHarness(t)
	m := bus.NewModule()
	testza.AssertNoError(t, m.Init(h.Ctx()))

	b, err := do.Invoke[*bus.Bus](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)

	testza.AssertNoError(t, m.Shutdown(h.Ctx()))

	testza.AssertErrorIs(t, bus.Publish(h.Ctx(), b, userCreated{ID: 1}), bus.ErrBusClosed)
}
