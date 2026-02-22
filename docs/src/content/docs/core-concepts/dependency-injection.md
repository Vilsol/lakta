---
title: Dependency Injection
description: How Lakta uses samber/do for context-carried dependency injection.
---

Lakta uses [`github.com/samber/do/v2`](https://github.com/samber/do) for dependency injection. The injector lives on the context, so it flows naturally through the module lifecycle and into request handlers.

## Registering a provider

```go
func (m *MyModule) Init(ctx context.Context) error {
    lakta.Provide(ctx, func(i do.Injector) (*MyService, error) {
        return &MyService{}, nil
    })
    return nil
}
```

Providers are lazy — they are only constructed when first invoked.

## Invoking from a handler

The idiomatic way to consume DI in gRPC and HTTP handlers is `lakta.Invoke[T]`, which takes the request context directly:

```go
// gRPC handler
func (s *MyServer) GetThing(ctx context.Context, req *pb.GetThingRequest) (*pb.GetThingResponse, error) {
    svc, err := lakta.Invoke[*MyService](ctx)
    if err != nil {
        return nil, err
    }
    return svc.GetThing(ctx, req.Id)
}

// HTTP handler (Fiber)
func handleGet(c fiber.Ctx) error {
    svc, err := lakta.Invoke[*MyService](c.Context())
    if err != nil {
        return err
    }
    return c.JSON(svc.Get())
}
```

## Invoking during Init

When wiring modules together in `Init`, retrieve the injector explicitly:

```go
func (m *MyModule) Init(ctx context.Context) error {
    injector := lakta.GetInjector(ctx)
    dep := do.MustInvoke[*Dependency](injector)
    m.svc = NewMyService(dep)
    return nil
}
```

## Typed client registration

Modules like `grpc/client` register a typed client directly when you provide a constructor via `WithClient`. This means you invoke the interface type, not the raw connection:

```go
// Registration (in NewRuntime)
grpcclient.NewModule(
    grpcclient.WithName("data"),
    grpcclient.WithClient(v1.NewDataServiceClient),
)

// Usage in any handler
client, err := lakta.Invoke[v1.DataServiceClient](ctx)
```

## Lifecycle

Providers are scoped to the injector's lifetime. When the runtime shuts down, `do` calls `Shutdown` on any provider that implements it, in reverse registration order.
