// api microservice
//   - TODO Connects to the data and orchestrator microservices.
//   - Exposes a HTTP server that serves the API.
//   - Exposes a HTTP server that serves health checks.
//   - TODO Exposes a HTTP server that serves metrics.
package main

import (
	"os"

	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/config"
	grpcclient "github.com/Vilsol/lakta/pkg/grpc/client"
	"github.com/Vilsol/lakta/pkg/health"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
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
		grpcclient.NewModule(
			grpcclient.WithName("data"),
			grpcclient.WithClient(v1.NewDataServiceClient),
		),
		grpcclient.NewModule(
			grpcclient.WithName("orchestrator"),
			grpcclient.WithClient(v1.NewWorkflowServiceClient),
		),
		fiberserver.NewModule(
			fiberserver.WithRouter(registerRoutes),
		),
	)

	if err := runtime.Run(); err != nil {
		os.Exit(1)
		return
	}
}
