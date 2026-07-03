package pool_test

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/Vilsol/lakta/pkg/workers/pool"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

const prefix = "modules.workers.pool.default.pools."

func setup(t *testing.T, data map[string]any, options ...pool.Option) (*pool.Registry, *pool.Module) {
	t.Helper()
	h := testkit.NewHarness(t)
	m := pool.NewModule(options...)

	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(data, "."), nil))
	testza.AssertNoError(t, m.LoadConfig(k))
	testza.AssertNoError(t, m.Init(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	reg, err := do.Invoke[*pool.Registry](lakta.GetInjector(h.Ctx()))
	testza.AssertNoError(t, err)
	return reg, m
}

func TestRegistry_GetKnownPoolExecutesTasks(t *testing.T) {
	t.Parallel()
	reg, _ := setup(t, map[string]any{
		prefix + "emails.workers":    2,
		prefix + "emails.queue_size": 8,
	})

	p, err := reg.Get("emails")
	testza.AssertNoError(t, err)

	done := make(chan struct{})
	testza.AssertNoError(t, p.Submit(t.Context(), func(_ context.Context) error {
		close(done)
		return nil
	}))
	waitFor(t, done)
}

func TestRegistry_GetUnknownPoolReturnsError(t *testing.T) {
	t.Parallel()
	reg, _ := setup(t, map[string]any{prefix + "emails.workers": 1})

	_, err := reg.Get("reports")

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "emails")
}

func TestModule_WithPoolCodeOption(t *testing.T) {
	t.Parallel()
	reg, _ := setup(t, map[string]any{}, pool.WithPool("emails", pool.PoolConfig{Workers: 1}))

	_, err := reg.Get("emails")
	testza.AssertNoError(t, err)
}

func TestModule_ConfigAndCodePoolsMerge(t *testing.T) {
	t.Parallel()
	reg, _ := setup(t,
		map[string]any{prefix + "reports.workers": 1},
		pool.WithPool("emails", pool.PoolConfig{Workers: 1}),
	)

	_, err := reg.Get("emails")
	testza.AssertNoError(t, err)
	_, err = reg.Get("reports")
	testza.AssertNoError(t, err)
}

func TestModule_ConfigPath(t *testing.T) {
	t.Parallel()
	testza.AssertEqual(t, "modules.workers.pool.default", pool.NewModule().ConfigPath())
	testza.AssertEqual(t, "modules.workers.pool.custom", pool.NewModule(pool.WithName("custom")).ConfigPath())
}

func TestModule_ShutdownClosesAllPools(t *testing.T) {
	t.Parallel()
	reg, m := setup(t, map[string]any{
		prefix + "emails.workers":  1,
		prefix + "reports.workers": 1,
	})

	testza.AssertNoError(t, m.Shutdown(t.Context()))

	for _, name := range []string{"emails", "reports"} {
		p, err := reg.Get(name)
		testza.AssertNoError(t, err)
		err = p.Submit(t.Context(), func(_ context.Context) error { return nil })
		testza.AssertErrorIs(t, err, pool.ErrPoolClosed)
	}
}
