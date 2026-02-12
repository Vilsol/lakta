// orchestrator microservice
//   - Connects to the data microservice.
//   - Exposes a gRPC server that serves health checks.
//   - Exposes a HTTP server that serves metrics.
package main

import (
	"context"
	"os"

	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/config"
	grpcclient "github.com/Vilsol/lakta/pkg/grpc/client"
	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/health"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/logging/slog"
	"github.com/Vilsol/lakta/pkg/logging/tint"
	"github.com/Vilsol/lakta/pkg/otel"
	"github.com/Vilsol/lakta/pkg/workflows/temporal"
	"go.temporal.io/sdk/worker"
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
		temporal.NewModule(
			temporal.WithRegistrar(func(ctx context.Context, w worker.Worker) error {
				w.RegisterWorkflow(SingleOrderWorkflow)
				w.RegisterActivity(UpdateOrderStatusActivity)
				return nil
			}),
		),
		grpcserver.NewModule(
			grpcserver.WithService(&v1.WorkflowService_ServiceDesc, NewServer()),
		),
	)

	if err := runtime.Run(); err != nil {
		os.Exit(1)
		return
	}
}
