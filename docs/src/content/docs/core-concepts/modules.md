---
title: Modules
description: The Module interface and its variants — sync, async, configurable, named, provider, and dependent.
---

Every piece of functionality in Lakta is a **module**. A module is a Go struct that implements one or more module interfaces.

## Base interface

```go
type Module interface {
    Init(ctx context.Context) error
    Shutdown(ctx context.Context) error
}
```

Use `Init` to register DI providers and wire up dependencies. Use `Shutdown` to release resources.

## SyncModule

For long-running services that must block (HTTP servers, gRPC servers):

```go
type SyncModule interface {
    Module
    Start(ctx context.Context) error
}
```

`Start` runs concurrently with other modules and is expected to block until shutdown.

## AsyncModule

For background workers that run without blocking:

```go
type AsyncModule interface {
    Module
    StartAsync(ctx context.Context) error
}
```

`StartAsync` must return quickly; spawn goroutines inside it.

## Provider

Declares what types this module registers in DI. The runtime uses this to resolve Init order automatically:

```go
type Provider interface {
    Provides() []reflect.Type
}
```

```go
func (m *MyModule) Provides() []reflect.Type {
    return []reflect.Type{reflect.TypeOf((*MyService)(nil))}
}
```

## Dependent

Declares what types this module needs before its `Init` runs:

```go
type Dependent interface {
    Dependencies() (required, optional []reflect.Type)
}
```

```go
func (m *MyModule) Dependencies() (required, optional []reflect.Type) {
    required = []reflect.Type{reflect.TypeOf((*pgxpool.Pool)(nil))}
    optional = []reflect.Type{reflect.TypeOf((*slog.Logger)(nil))}
    return
}
```

**Required** deps that have no registered provider cause a startup error before any `Init` fires. **Optional** deps are silently skipped if unavailable.

Together, `Provider` and `Dependent` let the runtime topologically sort modules — you don't need to pass them to `NewRuntime` in any particular order. See [Module Lifecycle](/getting-started/lifecycle) for details.

## Configurable

Modules that load from the config file:

```go
type Configurable interface {
    ConfigPath() string
    LoadConfig(*koanf.Koanf) error
}
```

The runtime calls `LoadConfig` automatically before `Init` using the sub-tree at `ConfigPath()`.

## NamedModule

Enables multiple instances of the same module type:

```go
type NamedModule interface {
    Name() string
}
```

Embed `lakta.NamedBase` and call `NewNamedBase(name)` for a ready-made implementation. See [Multi-instance Modules](/core-concepts/multi-instance).

## SyncCtx

When an interceptor set up during `Init` needs the runtime context (only available at `Start` time), embed `lakta.SyncCtx`:

```go
type MyModule struct {
    lakta.SyncCtx
}

func (m *MyModule) Init(ctx context.Context) error {
    // register an interceptor that calls m.RuntimeCtx() later
    return nil
}
```

The runtime injects the context before calling `Start`.
