// data microservice
//   - Connects to a PostgreSQL database.
//   - Exposes a gRPC server that serves database entities.
//   - Exposes a gRPC server that serves health checks.
//
// A `migrate` subcommand applies the embedded schema migrations out-of-band
// (init-container / prod path) and exits, without starting the runtime.
package main

import (
	"context"
	"embed"
	"log/slog"
	"os"

	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/db/drivers/pgx"
	"github.com/Vilsol/lakta/pkg/db/sql"
	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/health"
	"github.com/Vilsol/lakta/pkg/lakta"
	loggingslog "github.com/Vilsol/lakta/pkg/logging/slog"
	"github.com/Vilsol/lakta/pkg/logging/tint"
	"github.com/Vilsol/lakta/pkg/otel"
	"github.com/Vilsol/slox"
	"github.com/samber/do/v2"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const pgxInstance = "main"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(); err != nil {
			os.Exit(1)
		}
		return
	}

	runtime := lakta.NewRuntime(
		// Config module MUST be first
		config.NewModule(
			config.WithConfigDirs(".", "./config"),
			config.WithArgs(os.Args[1:]),
		),

		tint.NewModule(),
		loggingslog.NewModule(),
		otel.NewModule(),
		health.NewModule(),
		pgx.NewModule(pgx.WithName(pgxInstance), pgx.WithMigrations(migrationsFS)),
		sql.NewModule(),
		grpcserver.NewModule(
			grpcserver.WithService(&v1.DataService_ServiceDesc, NewServer()),
		),
	)

	if err := runtime.Run(); err != nil {
		os.Exit(1)
		return
	}
}

// runMigrate loads this service's pgx config from the standard config sources
// and applies the embedded migrations via the same code path as StartAsync,
// then exits. This is the production/init-container migration entrypoint.
func runMigrate() error {
	ctx := lakta.WithInjector(context.Background(), do.New())

	cfgModule := config.NewModule(
		config.WithConfigDirs(".", "./config"),
		config.WithArgs(os.Args[2:]),
	)
	if err := cfgModule.Init(ctx); err != nil {
		slox.Error(ctx, "failed to load config", slog.Any("error", err))
		return err
	}

	cfg := pgx.NewConfig(pgx.WithName(pgxInstance))
	if err := cfg.LoadFromKoanf(cfgModule.Koanf(), config.ModulePath(config.CategoryDB, "pgx", pgxInstance)); err != nil {
		slox.Error(ctx, "failed to load pgx config", slog.Any("error", err))
		return err
	}

	if err := pgx.RunMigrations(ctx, &cfg, migrationsFS); err != nil {
		slox.Error(ctx, "failed to run migrations", slog.Any("error", err))
		return err
	}

	slox.Info(ctx, "migrations applied")
	return nil
}
