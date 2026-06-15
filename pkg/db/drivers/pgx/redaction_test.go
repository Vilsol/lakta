package pgx_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	pkgpgx "github.com/Vilsol/lakta/pkg/db/drivers/pgx"
	"github.com/Vilsol/lakta/pkg/testkit"
)

// TestPGXModule_StartAsync_ConnectError_DoesNotLeakPassword guards the
// connection-error path: the DSN embeds the password, and the runtime logs
// StartAsync errors, so the error must never carry the raw DSN. WithMaxOpenConns(0)
// makes pgxpool.NewWithConfig fail synchronously ("MaxSize must be >= 1"),
// exercising the exact error path without needing a database.
func TestPGXModule_StartAsync_ConnectError_DoesNotLeakPassword(t *testing.T) {
	t.Parallel()

	const password = "sup3rs3cretPW"

	dsn := "postgres://dbuser:" + password + "@localhost:5432/appdb"
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn), pkgpgx.WithMaxOpenConns(0))
	h := testkit.NewHarness(t)

	testza.AssertNil(t, m.Init(h.Ctx()))

	err := m.StartAsync(h.Ctx())
	testza.AssertNotNil(t, err)

	// Neither the message nor the structured (logged) form may contain the password.
	verbose := fmt.Sprintf("%+v", err)
	testza.AssertFalse(t, strings.Contains(err.Error(), password))
	testza.AssertFalse(t, strings.Contains(verbose, password))

	// Non-sensitive connection context should still be attached for debugging.
	testza.AssertTrue(t, strings.Contains(verbose, "appdb"))
}
