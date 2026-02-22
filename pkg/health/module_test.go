package health_test

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/health"
	"github.com/Vilsol/lakta/pkg/testkit"
	healthgo "github.com/hellofresh/health-go/v5"
	"github.com/samber/do/v2"
)

func TestHealthModule_ProvidesHealth(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	m := health.NewModule()

	testza.AssertNil(t, m.Init(h.Ctx()))

	instance, err := do.Invoke[*healthgo.Health](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, instance)
}

func TestHealthModule_WithComponentName(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	m := health.NewModule(health.WithComponentName("my-service"), health.WithComponentVersion("1.0.0"))

	testza.AssertNil(t, m.Init(h.Ctx()))

	instance, err := do.Invoke[*healthgo.Health](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, instance)
}

func TestHealthModule_WithCheck(t *testing.T) {
	t.Parallel()

	checkRan := false
	h := testkit.NewHarness(t)
	m := health.NewModule(health.WithCheck(healthgo.Config{
		Name: "test-check",
		Check: func(ctx context.Context) error {
			checkRan = true
			return nil
		},
	}))

	testza.AssertNil(t, m.Init(h.Ctx()))

	instance, err := do.Invoke[*healthgo.Health](h.Injector())
	testza.AssertNil(t, err)

	result := instance.Measure(context.Background())
	testza.AssertTrue(t, checkRan)
	testza.AssertNotNil(t, result)
}

func TestHealthModule_ShutdownNoop(t *testing.T) {
	t.Parallel()

	m := health.NewModule()
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestHealthModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.health.health.default", health.NewModule().ConfigPath())
}

func TestHealthModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", health.NewModule().Name())
	testza.AssertEqual(t, "custom", health.NewModule(health.WithName("custom")).Name())
}

func TestHealthModule_NoKoanfSucceeds(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	testza.AssertNil(t, health.NewModule().Init(h.Ctx()))
}
