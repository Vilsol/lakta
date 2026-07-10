// Schema migrations for the pgx module, driven by pressly/goose v3.
//
// Non-transactional migration wedge risk: goose runs each migration in a
// transaction by default, but statements like CREATE INDEX CONCURRENTLY cannot
// run inside a transaction and require the `-- +goose NO TRANSACTION` file
// annotation. A non-transactional migration that fails partway leaves a partial
// schema goose cannot auto-roll-back; combined with the advisory lock on a
// replica-set on-start run, every instance then retries the same broken
// migration and fails health, wedging startup. Prefer the out-of-band
// RunMigrations path (mise run migrate) for any migration containing
// CONCURRENTLY or other non-transactional DDL, and keep run_on_start false in
// production.

package pgx

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"

	"github.com/Vilsol/slox"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/samber/oops"
)

// RunMigrations opens a pool and *sql.DB from cfg, runs goose Up, then closes
// them. It is the same migration code path as StartAsync (via
// Config.GooseProvider), callable outside the runtime from an app's main gated
// on a "migrate" subcommand. It is a no-op when fsys is nil.
func RunMigrations(ctx context.Context, cfg *Config, fsys fs.FS) error {
	if fsys == nil {
		return nil
	}

	cfg.logLevelParsed = cfg.ParseLogLevel()

	poolConfig, err := cfg.NewPoolConfig()
	if err != nil {
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return oops.
			With("host", poolConfig.ConnConfig.Host).
			With("database", poolConfig.ConnConfig.Database).
			Wrapf(err, "failed to connect to database")
	}
	defer pool.Close()

	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	return runMigrations(ctx, cfg, db, fsys)
}

// runMigrations builds the goose provider from cfg and applies pending
// migrations against db, logging applied versions. Shared by StartAsync (which
// owns the pool) and RunMigrations (which opens its own).
func runMigrations(ctx context.Context, cfg *Config, db *sql.DB, fsys fs.FS) error {
	provider, err := cfg.GooseProvider(db, fsys)
	if err != nil {
		return oops.Wrapf(err, "failed to build migration provider")
	}
	if provider == nil {
		return nil
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return oops.Wrapf(err, "failed to run migrations")
	}

	for _, r := range results {
		version := int64(0)
		source := ""
		if r.Source != nil {
			version = r.Source.Version
			source = r.Source.Path
		}
		slox.Info(
			ctx,
			"applied database migration",
			slog.Int64("version", version),
			slog.String("source", source),
			slog.Duration("duration", r.Duration),
		)
	}

	return nil
}
