package lakta_test

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
)

func TestGetInjector_PanicsWithoutInjector(t *testing.T) {
	t.Parallel()

	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		lakta.GetInjector(context.Background())
	}()

	testza.AssertTrue(t, panicked)
}

func TestWithInjector_GetInjector_RoundTrip(t *testing.T) {
	t.Parallel()

	injector := do.New()
	ctx := lakta.WithInjector(context.Background(), injector)
	got := lakta.GetInjector(ctx)
	testza.AssertEqual(t, injector, got)
}

func TestProvide_RegistersValue(t *testing.T) {
	t.Parallel()

	injector := do.New()
	ctx := lakta.WithInjector(context.Background(), injector)

	lakta.Provide(ctx, func(_ do.Injector) (string, error) {
		return "hello", nil
	})

	val, err := do.Invoke[string](injector)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "hello", val)
}
