package pgx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	pkgpgx "github.com/Vilsol/lakta/pkg/db/drivers/pgx"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/Vilsol/lakta/pkg/workers/pool"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/do/v2"
)

// setupTxTest spins up a Postgres container, wires the pgx module into a harness
// injector, and returns a ctx carrying that injector plus the live pool. The
// ctx is what Q/WithTx resolve their pool from. A single-column items table is
// created for insert/visibility assertions.
func setupTxTest(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

	dsn := startPostgresContainer(t)
	h := testkit.NewHarness(t)
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	poolConn, err := do.Invoke[*pgxpool.Pool](h.Injector())
	testza.AssertNil(t, err)

	_, err = poolConn.Exec(h.Ctx(), "CREATE TABLE items (id int PRIMARY KEY)")
	testza.AssertNil(t, err)

	return h.Ctx(), poolConn
}

func poolCount(t *testing.T, ctx context.Context, p *pgxpool.Pool, id int) int {
	t.Helper()
	var n int
	testza.AssertNil(t, p.QueryRow(ctx, "SELECT count(*) FROM items WHERE id = $1", id).Scan(&n))
	return n
}

func TestWithTx_CommitPath(t *testing.T) {
	t.Parallel()

	ctx, poolConn := setupTxTest(t)

	err := pkgpgx.WithTx(ctx, func(ctx context.Context) error {
		_, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (1)")
		return err //nolint:wrapcheck // surface the raw pgx error to WithTx unchanged
	})
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 1, poolCount(t, ctx, poolConn, 1))
}

func TestWithTx_RollbackPath(t *testing.T) {
	t.Parallel()

	ctx, poolConn := setupTxTest(t)

	sentinel := errors.New("boom")
	err := pkgpgx.WithTx(ctx, func(ctx context.Context) error {
		if _, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (2)"); err != nil {
			return err //nolint:wrapcheck // surface the raw pgx error to WithTx unchanged
		}
		return sentinel
	})
	testza.AssertTrue(t, errors.Is(err, sentinel))

	testza.AssertEqual(t, 0, poolCount(t, ctx, poolConn, 2))
}

func TestWithTx_PanicPath(t *testing.T) {
	t.Parallel()

	ctx, poolConn := setupTxTest(t)

	testza.AssertPanics(t, func() {
		_ = pkgpgx.WithTx(ctx, func(ctx context.Context) error {
			_, _ = pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (3)")
			panic("kaboom")
		})
	})

	// Rolled back: row not visible.
	testza.AssertEqual(t, 0, poolCount(t, ctx, poolConn, 3))

	// State is done: Q on the same ctx now returns the pool and a query works.
	var one int
	testza.AssertNil(t, pkgpgx.Q(ctx).QueryRow(ctx, "SELECT 1").Scan(&one))
	testza.AssertEqual(t, 1, one)
}

func TestWithTx_SavepointNesting(t *testing.T) {
	t.Parallel()

	ctx, poolConn := setupTxTest(t)

	err := pkgpgx.WithTx(ctx, func(ctx context.Context) error {
		if _, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (10)"); err != nil {
			return err //nolint:wrapcheck // surface the raw pgx error to WithTx unchanged
		}

		// Inner nested tx inserts B then errors: rolls back to savepoint only.
		innerErr := pkgpgx.WithTx(ctx, func(ctx context.Context) error {
			if _, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (11)"); err != nil {
				return err //nolint:wrapcheck // surface the raw pgx error to WithTx unchanged
			}
			return errors.New("inner fails")
		})
		testza.AssertNotNil(t, innerErr)

		return nil
	})
	testza.AssertNil(t, err)

	testza.AssertEqual(t, 1, poolCount(t, ctx, poolConn, 10)) // A committed
	testza.AssertEqual(t, 0, poolCount(t, ctx, poolConn, 11)) // B rolled back
}

func TestWithTx_DeadTxGuard(t *testing.T) {
	t.Parallel()

	ctx, _ := setupTxTest(t)

	p := pool.New(pool.PoolConfig{Workers: 1})
	t.Cleanup(func() { _ = p.Close(context.Background()) })

	release := make(chan struct{})
	ran := make(chan error, 1)

	err := pkgpgx.WithTx(ctx, func(txCtx context.Context) error {
		// Submit a task that copies the tx ctx value via WithoutCancel and runs
		// only after the outer WithTx has committed.
		return p.Submit(txCtx, func(taskCtx context.Context) error {
			<-release // block until the outer tx has committed
			q := pkgpgx.Q(taskCtx)
			// Must be the pool, not the finished tx: a query must succeed.
			var one int
			ran <- q.QueryRow(taskCtx, "SELECT 1").Scan(&one)
			return nil
		})
	})
	testza.AssertNil(t, err)

	close(release) // outer tx committed; let the detached task run

	select {
	case taskErr := <-ran:
		testza.AssertNil(t, taskErr)
	case <-time.After(10 * time.Second):
		t.Fatal("detached task did not run")
	}
}

func TestQ_FallbackReturnsPool(t *testing.T) {
	t.Parallel()

	ctx, _ := setupTxTest(t)

	var one int
	testza.AssertNil(t, pkgpgx.Q(ctx).QueryRow(ctx, "SELECT 1").Scan(&one))
	testza.AssertEqual(t, 1, one)
}

func TestWithTx_ReadOnlyRejectsWrite(t *testing.T) {
	t.Parallel()

	ctx, _ := setupTxTest(t)

	err := pkgpgx.WithTx(ctx, func(ctx context.Context) error {
		_, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (99)")
		return err //nolint:wrapcheck // surface the raw pgx read-only error to WithTx unchanged
	}, pkgpgx.WithReadOnly())

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "read-only")
}

func TestWithNewTx_ForcesFreshTopLevel(t *testing.T) {
	t.Parallel()

	ctx, poolConn := setupTxTest(t)

	err := pkgpgx.WithTx(ctx, func(ctx context.Context) error {
		if _, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (20)"); err != nil {
			return err //nolint:wrapcheck // surface the raw pgx error to WithTx unchanged
		}

		// A fresh top-level tx that commits independently of the outer one.
		if newErr := pkgpgx.WithNewTx(ctx, func(ctx context.Context) error {
			_, err := pkgpgx.Q(ctx).Exec(ctx, "INSERT INTO items (id) VALUES (21)")
			return err //nolint:wrapcheck // surface the raw pgx error to WithNewTx unchanged
		}); newErr != nil {
			return newErr //nolint:wrapcheck // surface the raw WithNewTx error unchanged
		}

		// Outer rolls back after the new tx already committed.
		return errors.New("outer fails")
	})
	testza.AssertNotNil(t, err)

	testza.AssertEqual(t, 0, poolCount(t, ctx, poolConn, 20)) // outer rolled back
	testza.AssertEqual(t, 1, poolCount(t, ctx, poolConn, 21)) // new tx committed independently
}
