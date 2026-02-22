---
title: Microservices Example
description: Walkthrough of the examples/microservices setup — data service, orchestrator, and API gateway.
---

The `examples/microservices/` directory contains a three-service food-ordering system that demonstrates real-world Lakta usage end-to-end.

```
examples/microservices/
├── data/          # PostgreSQL-backed gRPC service for all entities
├── orchestrator/  # Temporal worker + gRPC facade for order workflows
└── api/           # Fiber HTTP gateway — translates REST to gRPC calls
```

A shared `lakta.yaml` at the root provides config for all three services.

## data service

Owns the PostgreSQL database and exposes a gRPC API for CRUD operations on restaurants, customers, orders, and menus.

```go
lakta.NewRuntime(
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
```

The `sql` module wraps the pgx connection in a `*squirrel.StatementBuilderType` and registers it in DI. gRPC handlers access it directly:

```go
func (s *DataServer) ListRestaurants(ctx context.Context, req *v1.ListRestaurantsRequest) (*v1.ListRestaurantsResponse, error) {
    db, err := lakta.Invoke[*squirrel.StatementBuilderType](ctx)
    if err != nil {
        return nil, err
    }
    rows, err := db.Select("id", "name", "cuisine_type", "address", "created_at").
        From("restaurants").
        Limit(req.Limit).
        Offset(req.Offset).
        QueryContext(ctx)
    // ...
}
```

## orchestrator service

Runs a Temporal worker for `SingleOrderWorkflow` and exposes a gRPC `WorkflowService` that triggers workflows. It also connects to the data service as a gRPC client so activities can update order status.

```go
lakta.NewRuntime(
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
```

The gRPC handler starts a workflow, and the activity uses the data client to update state:

```go
// gRPC handler triggers a workflow
func (s *WorkflowServer) StartOrderWorkflow(ctx context.Context, req *v1.StartOrderWorkflowRequest) (*v1.StartOrderWorkflowResponse, error) {
    c, err := lakta.Invoke[client.Client](ctx)
    if err != nil {
        return nil, status.Error(codes.Internal, err.Error())
    }
    _, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
        ID:        fmt.Sprintf("order-sequence-workflow-%s", req.GetOrderId()),
        TaskQueue: taskQueue,
    }, SingleOrderWorkflow, req.GetOrderId())
    // ...
}

// Activity calls back into the data service
func UpdateOrderStatusActivity(ctx context.Context, orderID string, newStatus v1.OrderStatus) error {
    client, err := lakta.Invoke[v1.DataServiceClient](ctx)
    if err != nil {
        return err
    }
    _, err = client.UpdateOrderStatus(ctx, &v1.UpdateOrderStatusRequest{
        OrderId:   orderID,
        NewStatus: newStatus,
    })
    return err
}
```

## api service

An HTTP gateway that receives REST requests and fans out to both the data and orchestrator gRPC services.

```go
lakta.NewRuntime(
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
        fiberserver.WithDefaults(fiber.Config{
            ReadTimeout:  30 * time.Second,
            WriteTimeout: 30 * time.Second,
        }),
        fiberserver.WithRouter(registerRoutes),
    ),
)
```

Routes call both gRPC clients. For example, placing an order writes to the data service then triggers the orchestrator workflow:

```go
func postOrder(c fiber.Ctx) error {
    dataClient, err := lakta.Invoke[v1.DataServiceClient](c.Context())
    if err != nil {
        return err
    }
    workflowClient, err := lakta.Invoke[v1.WorkflowServiceClient](c.Context())
    if err != nil {
        return err
    }

    resp, err := dataClient.CreateOrder(c.Context(), &v1.CreateOrderRequest{...})
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
    }

    if _, err := workflowClient.StartOrderWorkflow(c.Context(), &v1.StartOrderWorkflowRequest{
        OrderId: resp.GetOrder().GetId(),
    }); err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
    }

    return c.JSON(fiber.Map{"order": resp.GetOrder()})
}
```

## Shared config (lakta.yaml)

```yaml
modules:
  otel:
    otel:
      default:
        service_name: "microservices-demo"

  logging:
    tint:
      default:
        level: "debug"

  health:
    health:
      default:
        component_name: "microservices-demo"
        component_version: "v1.0.0"

  grpc:
    server:
      default:
        host: "0.0.0.0"
        port: 50051
        health_check: true
    client:
      data:
        target: "localhost:50051"
        insecure: true
      orchestrator:
        target: "localhost:50052"
        insecure: true

  http:
    fiber:
      default:
        host: "0.0.0.0"
        port: 8080
        health_path: "/health"

  db:
    pgx:
      main:
        dsn: "postgres://postgres:foo@localhost:5432/postgres?sslmode=disable"
        max_open_conns: 25
        log_level: "info"
        health_check: true

  workflows:
    temporal:
      default:
        target: "localhost:7233"
        task_queue: "EXAMPLE_ORCHESTRATOR_QUEUE"
        namespace: "default"
        insecure: true
```
