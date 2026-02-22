package sql_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Masterminds/squirrel"
	pkgsql "github.com/Vilsol/lakta/pkg/db/sql"
	"github.com/Vilsol/lakta/pkg/testkit"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/samber/do/v2"
)

func TestSQLModule_Init_RegistersProvider(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", "postgres://test:test@localhost:5432/test")
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = db.Close() })

	h := testkit.NewHarness(t)
	testkit.WithProvider(h, func(_ do.Injector) (*sql.DB, error) {
		return db, nil
	})

	m := pkgsql.NewModule()
	testza.AssertNil(t, m.Init(h.Ctx()))

	builder, invokeErr := do.Invoke[*squirrel.StatementBuilderType](h.Injector())
	testza.AssertNil(t, invokeErr)
	testza.AssertNotNil(t, builder)
}

func TestSQLModule_Shutdown_IsNoOp(t *testing.T) {
	t.Parallel()

	m := pkgsql.NewModule()
	testza.AssertNil(t, m.Shutdown(context.Background()))
}
