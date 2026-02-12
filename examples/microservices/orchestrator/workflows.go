package main

import (
	"context"
	"time"

	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"go.temporal.io/sdk/workflow"
)

func SingleOrderWorkflow(ctx workflow.Context, orderID string) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
	})

	// Transition to "CONFIRMED"
	if err := workflow.ExecuteActivity(ctx, UpdateOrderStatusActivity, orderID, v1.OrderStatus_ORDER_STATUS_CONFIRMED).Get(ctx, nil); err != nil {
		return err
	}

	// Transition to "PREPARING"
	if err := workflow.ExecuteActivity(ctx, UpdateOrderStatusActivity, orderID, v1.OrderStatus_ORDER_STATUS_PREPARING).Get(ctx, nil); err != nil {
		return err
	}

	// Transition to "READY"
	if err := workflow.ExecuteActivity(ctx, UpdateOrderStatusActivity, orderID, v1.OrderStatus_ORDER_STATUS_READY).Get(ctx, nil); err != nil {
		return err
	}

	return nil
}

func UpdateOrderStatusActivity(ctx context.Context, orderID string, orderStatus v1.OrderStatus) error {
	client, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(ctx))
	if err != nil {
		return err
	}

	_, err = client.UpdateOrderStatus(ctx, &v1.UpdateOrderStatusRequest{
		OrderId:   orderID,
		NewStatus: orderStatus,
	})
	if err != nil {
		return oops.Wrapf(err, "failed to update order status")
	}

	return nil
}
