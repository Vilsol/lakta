// data microservice
//   - Connects to a PostgreSQL database.
//   - Exposes a gRPC server that serves database entities.
//   - Exposes a gRPC server that serves health checks.
//   - TODO Exposes a HTTP server that serves metrics.
package main

import (
	"os"

	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/db/drivers/pgx"
	"github.com/Vilsol/lakta/pkg/db/sql"
	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/health"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/logging/slog"
	"github.com/Vilsol/lakta/pkg/logging/tint"
	"github.com/Vilsol/lakta/pkg/otel"
)

func main() {
	runtime := lakta.NewRuntime(
		// Config module MUST be first
		config.NewModule(
			config.WithConfigDirs(".", "./config"),
			config.WithArgs(os.Args[1:]),
		),

		tint.NewModule(),
		slog.NewModule(),
		otel.NewModule(),
		health.NewModule(),
		pgx.NewModule(pgx.WithName("main")),
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
