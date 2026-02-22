package pgx_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	pkgpgx "github.com/Vilsol/lakta/pkg/db/drivers/pgx"
	"github.com/Vilsol/lakta/pkg/testkit"
	healthgo "github.com/hellofresh/health-go/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/do/v2"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPostgresContainer(t *testing.T) string {
	t.Helper()

	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("postgres container unavailable: %v", err)
	}

	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	testza.AssertNil(t, err)

	return dsn
}

func TestPGXModule_ConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.db.pgx.default", pkgpgx.NewModule().ConfigPath())
}

func TestPGXModule_Name(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "default", pkgpgx.NewModule().Name())
	testza.AssertEqual(t, "custom", pkgpgx.NewModule(pkgpgx.WithName("custom")).Name())
}

func TestPGXModule_StartAsync_RegistersPool(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	h := testkit.NewHarness(t)
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	pool, err := do.Invoke[*pgxpool.Pool](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, pool)
}

func TestPGXModule_StartAsync_RegistersSQLDB(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	h := testkit.NewHarness(t)
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	db, err := do.Invoke[*sql.DB](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, db)
}

func TestPGXModule_StartAsync_RegistersHealthCheck(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	h := testkit.NewHarness(t)

	healthInstance, err := healthgo.New()
	testza.AssertNil(t, err)
	testkit.WithProvider(h, func(_ do.Injector) (*healthgo.Health, error) {
		return healthInstance, nil
	})

	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn), pkgpgx.WithHealthCheck(true))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))

	// Verify postgres check passes while pool is open.
	result := healthInstance.Measure(context.Background())
	testza.AssertEqual(t, healthgo.StatusOK, result.Status)
	testza.AssertEqual(t, 0, len(result.Failures))

	// Shut down the pool and confirm the postgres check now fails.
	testza.AssertNil(t, m.Shutdown(context.Background()))

	result = healthInstance.Measure(context.Background())
	testza.AssertNotEqual(t, healthgo.StatusOK, result.Status)
	testza.AssertTrue(t, len(result.Failures["postgres"]) > 0)
}

func TestPGXModule_Shutdown_ClosesPool(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	h := testkit.NewHarness(t)
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))

	pool, err := do.Invoke[*pgxpool.Pool](h.Injector())
	testza.AssertNil(t, err)

	if pool == nil {
		t.FailNow()
	}

	testza.AssertNil(t, m.Shutdown(context.Background()))

	pingCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	testza.AssertNotNil(t, pool.Ping(pingCtx))
}
