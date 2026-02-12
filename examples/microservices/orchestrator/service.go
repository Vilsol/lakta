package main

import (
	"context"
	"fmt"

	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WorkflowServer struct {
	v1.UnimplementedWorkflowServiceServer
}

func NewServer() *WorkflowServer {
	return &WorkflowServer{}
}

func (s *WorkflowServer) StartOrderWorkflow(ctx context.Context, request *v1.StartOrderWorkflowRequest) (*v1.StartOrderWorkflowResponse, error) {
	c, err := do.Invoke[client.Client](lakta.GetInjector(ctx))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if _, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("order-sequence-workflow-%s", request.GetOrderId()),
		TaskQueue: taskQueue,
	}, SingleOrderWorkflow, request.GetOrderId()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return nil, nil
}

func (s *WorkflowServer) CompleteOrder(ctx context.Context, request *v1.CompleteOrderRequest) (*v1.CompleteOrderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CompleteOrder not implemented")
}
