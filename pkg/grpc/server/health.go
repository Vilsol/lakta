package grpcserver

import (
	"context"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/hellofresh/health-go/v5"
	"github.com/samber/do/v2"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type healthServer struct {
	healthpb.UnimplementedHealthServer

	runtimeContext context.Context //nolint:containedctx
}

func newHealthServer(ctx context.Context) *healthServer {
	return &healthServer{runtimeContext: ctx}
}

func (s *healthServer) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	h, err := do.Invoke[*health.Health](lakta.GetInjector(s.runtimeContext))
	if err != nil {
		return &healthpb.HealthCheckResponse{
			Status: healthpb.HealthCheckResponse_NOT_SERVING,
		}, nil
	}

	result := h.Measure(ctx)
	if result.Status == health.StatusOK {
		return &healthpb.HealthCheckResponse{
			Status: healthpb.HealthCheckResponse_SERVING,
		}, nil
	}

	return &healthpb.HealthCheckResponse{
		Status: healthpb.HealthCheckResponse_NOT_SERVING,
	}, nil
}

func (s *healthServer) Watch(_ *healthpb.HealthCheckRequest, _ healthpb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "watching is not supported") //nolint:wrapcheck
}
