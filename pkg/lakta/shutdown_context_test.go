package lakta_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
)

// TestRuntime_ShutdownContext_PreservesInjectorAndLogger guards that the
// shutdown context still carries the DI injector and logger. The runtime
// detaches the shutdown context from the (already-cancelled) run context, but
// must preserve its values so modules can resolve dependencies and log during
// Shutdown. With a bare context.Background() the injector lookup panics.
func TestRuntime_ShutdownContext_PreservesInjectorAndLogger(t *testing.T) {
	t.Parallel()

	var injectorOK, loggerOK atomic.Bool

	m := testkit.NewMockModule()
	m.OnShutdown = func(ctx context.Context) error {
		// GetInjector panics when the injector is absent from the context.
		defer func() { _ = recover() }()

		_ = lakta.GetInjector(ctx)
		injectorOK.Store(true)

		if _, err := lakta.Invoke[*slog.Logger](ctx); err == nil {
			loggerOK.Store(true)
		}

		return nil
	}

	rh := testkit.NewRuntimeHarness(t, m)
	testza.AssertNil(t, rh.Shutdown())

	testza.AssertTrue(t, injectorOK.Load())
	testza.AssertTrue(t, loggerOK.Load())
}
