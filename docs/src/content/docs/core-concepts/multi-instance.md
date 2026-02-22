---
title: Multi-instance Modules
description: Running multiple instances of the same module type with NamedBase and WithName.
---

Some modules — particularly gRPC clients and database connections — are often needed in multiple configurations within a single service. Lakta supports this via `NamedModule`.

## How it works

A `NamedModule` registers its DI providers under its name rather than the type alone. This lets you have, say, two gRPC client connections with different targets.

## Using WithName

Every built-in module that supports multi-instance ships a `WithName` option:

```go
lakta.NewRuntime(
    grpcclient.NewModule(grpcclient.WithName("payments")),
    grpcclient.NewModule(grpcclient.WithName("notifications")),
)
```

Config for each instance lives under its name:

```yaml
modules:
  grpc:
    client:
      payments:
        target: "payments-svc:50051"
      notifications:
        target: "notifications-svc:50051"
```

## Retrieving named instances

Use `do.MustInvokeNamed` with the instance name:

```go
paymentsConn := do.MustInvokeNamed[*grpc.ClientConn](injector, "payments")
notifsConn   := do.MustInvokeNamed[*grpc.ClientConn](injector, "notifications")
```

## Implementing NamedBase in custom modules

```go
type MyModule struct {
    lakta.NamedBase
}

func NewMyModule(name string) *MyModule {
    m := &MyModule{}
    m.NamedBase = lakta.NewNamedBase(name)
    return m
}
```

`NamedBase` satisfies the `NamedModule` interface and stores the name for use in `ConfigPath()` and DI registration.
