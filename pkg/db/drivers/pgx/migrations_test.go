package pgx_test

import (
	"context"
	"database/sql"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/MarvinJWendt/testza"
	pkgpgx "github.com/Vilsol/lakta/pkg/db/drivers/pgx"
	"github.com/Vilsol/lakta/pkg/testkit"
	healthgo "github.com/hellofresh/health-go/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/samber/do/v2"
)

// gooseUp wraps a single DDL statement as an up-only goose SQL migration.
func gooseUp(stmt string) string {
	return "-- +goose Up\n" + stmt + "\n"
}

const migWidgets = "00001_widgets.sql"

// migrationFS builds an fs.FS whose "migrations/" sub-path holds the given
// files (name -> body), matching the //go:embed migrations/*.sql layout that
// Config.Dir defaults to.
func migrationFS(files map[string]string) fs.FS {
	m := fstest.MapFS{}
	for name, body := range files {
		m["migrations/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(dsn)
	testza.AssertNil(t, err)

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	testza.AssertNil(t, err)

	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() {
		_ = db.Close()
		pool.Close()
	})

	return db
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var reg sql.NullString
	err := db.QueryRowContext(context.Background(), "SELECT to_regclass($1)", name).Scan(&reg)
	testza.AssertNil(t, err)

	return reg.Valid
}

// countMigrations counts applied migration rows, excluding goose's version-0
// baseline row inserted when the history table is first created.
func countMigrations(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	err := db.QueryRowContext(context.Background(),
		"SELECT count(*) FROM schema_migrations WHERE version_id > 0").Scan(&n)
	testza.AssertNil(t, err)

	return n
}

// TestMigrations_GooseProviderNilFS verifies the nil-FS skip path without a DB.
func TestMigrations_GooseProviderNilFS(t *testing.T) {
	t.Parallel()

	cfg := pkgpgx.NewConfig(pkgpgx.WithDSN("postgres://ignored"))

	provider, err := cfg.GooseProvider(nil, nil)
	testza.AssertNil(t, err)
	testza.AssertNil(t, provider)
}

// TestMigrations_RunMigrationsNilFS verifies RunMigrations no-ops on nil FS
// (never opens a pool, so the DSN is irrelevant).
func TestMigrations_RunMigrationsNilFS(t *testing.T) {
	t.Parallel()

	cfg := pkgpgx.NewConfig(pkgpgx.WithDSN("postgres://ignored"))

	testza.AssertNil(t, pkgpgx.RunMigrations(context.Background(), &cfg, nil))
}

func TestMigrations_EmbeddedFSUp(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	db := openDB(t, dsn)

	fsys := migrationFS(map[string]string{
		migWidgets:          gooseUp("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"),
		"00002_gadgets.sql": gooseUp("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"),
	})

	cfg := pkgpgx.NewConfig(pkgpgx.WithDSN(dsn))

	provider, err := cfg.GooseProvider(db, fsys)
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, provider)

	_, err = provider.Up(context.Background())
	testza.AssertNil(t, err)

	testza.AssertTrue(t, tableExists(t, db, "widgets"))
	testza.AssertTrue(t, tableExists(t, db, "gadgets"))
	testza.AssertTrue(t, tableExists(t, db, "schema_migrations"))
	testza.AssertEqual(t, 2, countMigrations(t, db))
}

// TestMigrations_RunOnStartOffByDefault proves the prod-safe default: an FS is
// set but run_on_start is false, so StartAsync applies nothing.
func TestMigrations_RunOnStartOffByDefault(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	fsys := migrationFS(map[string]string{
		migWidgets: gooseUp("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"),
	})

	h := testkit.NewHarness(t)
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn), pkgpgx.WithMigrations(fsys))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	db, err := do.Invoke[*sql.DB](h.Injector())
	testza.AssertNil(t, err)

	testza.AssertFalse(t, tableExists(t, db, "schema_migrations"))
	testza.AssertFalse(t, tableExists(t, db, "widgets"))
}

// TestMigrations_RunOnStartOn proves migrations apply during StartAsync before
// health can report ready when run_on_start is enabled.
func TestMigrations_RunOnStartOn(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	fsys := migrationFS(map[string]string{
		migWidgets: gooseUp("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"),
	})

	h := testkit.NewHarness(t)

	healthInstance, err := healthgo.New()
	testza.AssertNil(t, err)
	testkit.WithProvider(h, func(_ do.Injector) (*healthgo.Health, error) {
		return healthInstance, nil
	})

	m := pkgpgx.NewModule(
		pkgpgx.WithDSN(dsn),
		pkgpgx.WithMigrations(fsys),
		pkgpgx.WithMigrationsRunOnStart(true),
		pkgpgx.WithHealthCheck(true),
	)

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	db, err := do.Invoke[*sql.DB](h.Injector())
	testza.AssertNil(t, err)

	testza.AssertTrue(t, tableExists(t, db, "widgets"))
	testza.AssertTrue(t, tableExists(t, db, "schema_migrations"))

	result := healthInstance.Measure(context.Background())
	testza.AssertEqual(t, healthgo.StatusOK, result.Status)
}

// TestMigrations_AdvisoryLockReplicaSafety runs two providers against the same
// DB concurrently under the advisory lock; both succeed and each migration is
// applied exactly once.
func TestMigrations_AdvisoryLockReplicaSafety(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	fsys := migrationFS(map[string]string{
		migWidgets:          gooseUp("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"),
		"00002_gadgets.sql": gooseUp("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"),
	})

	db1 := openDB(t, dsn)
	db2 := openDB(t, dsn)

	cfg := pkgpgx.NewConfig(pkgpgx.WithDSN(dsn))

	p1, err := cfg.GooseProvider(db1, fsys)
	testza.AssertNil(t, err)
	p2, err := cfg.GooseProvider(db2, fsys)
	testza.AssertNil(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() { defer wg.Done(); _, errs[0] = p1.Up(context.Background()) }()
	go func() { defer wg.Done(); _, errs[1] = p2.Up(context.Background()) }()
	wg.Wait()

	testza.AssertNil(t, errs[0])
	testza.AssertNil(t, errs[1])
	testza.AssertEqual(t, 2, countMigrations(t, db1))
}

// TestMigrations_AllowMissing proves the out-of-order toggle reaches goose:
// with allow_missing false an out-of-order migration errors, with true it applies.
func TestMigrations_AllowMissing(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)
	db := openDB(t, dsn)

	base := migrationFS(map[string]string{
		"00001_a.sql": gooseUp("CREATE TABLE a (id INTEGER);"),
		"00003_c.sql": gooseUp("CREATE TABLE c (id INTEGER);"),
	})
	full := migrationFS(map[string]string{
		"00001_a.sql": gooseUp("CREATE TABLE a (id INTEGER);"),
		"00002_b.sql": gooseUp("CREATE TABLE b (id INTEGER);"),
		"00003_c.sql": gooseUp("CREATE TABLE c (id INTEGER);"),
	})

	cfg := pkgpgx.NewConfig(pkgpgx.WithDSN(dsn))
	p, err := cfg.GooseProvider(db, base)
	testza.AssertNil(t, err)
	_, err = p.Up(context.Background())
	testza.AssertNil(t, err)

	// allow_missing=false: the out-of-order migration 2 errors.
	strict := pkgpgx.NewConfig(pkgpgx.WithDSN(dsn))
	pStrict, err := strict.GooseProvider(db, full)
	testza.AssertNil(t, err)
	_, err = pStrict.Up(context.Background())
	testza.AssertNotNil(t, err)
	testza.AssertFalse(t, tableExists(t, db, "b"))

	// allow_missing=true: migration 2 is applied.
	loose := pkgpgx.NewConfig(pkgpgx.WithDSN(dsn))
	loose.Migrations.AllowMissing = true
	pLoose, err := loose.GooseProvider(db, full)
	testza.AssertNil(t, err)
	_, err = pLoose.Up(context.Background())
	testza.AssertNil(t, err)
	testza.AssertTrue(t, tableExists(t, db, "b"))
}

// TestMigrations_NilFSStartAsync proves run_on_start with no FS set skips cleanly.
func TestMigrations_NilFSStartAsync(t *testing.T) {
	t.Parallel()

	dsn := startPostgresContainer(t)

	h := testkit.NewHarness(t)
	m := pkgpgx.NewModule(pkgpgx.WithDSN(dsn), pkgpgx.WithMigrationsRunOnStart(true))

	testza.AssertNil(t, m.Init(h.Ctx()))
	testza.AssertNil(t, m.StartAsync(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	db, err := do.Invoke[*sql.DB](h.Injector())
	testza.AssertNil(t, err)
	testza.AssertFalse(t, tableExists(t, db, "schema_migrations"))
}
