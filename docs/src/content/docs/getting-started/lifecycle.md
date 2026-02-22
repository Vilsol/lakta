---
title: Module Lifecycle
description: How Lakta initializes, starts, and shuts down modules.
---

## Phases

### 1. Dependency sort

Before any `Init` runs, the runtime topologically sorts all modules using their `Provider` and `Dependent` declarations (Kahn's algorithm). Modules that declare neither preserve their relative order.

If a module declares a required dependency with no registered provider, the runtime returns an error immediately — before any `Init` fires.

### 2. Init (sequential, dependency order)

Modules are initialized one at a time in the resolved order. By the time a module's `Init` runs, everything it declared as a dependency has already been registered in DI.

```
[sorted] config.Init() → tint.Init() → slog.Init() → otel.Init() → ...
```

`LoadConfig` is called automatically before `Init` for any module implementing `Configurable`.

### 3. Logger injection

After all inits succeed, the runtime retrieves `*slog.Logger` from DI and injects it into the context. All subsequent logging goes through this logger.

Hot-reload callbacks are also wired here for any `HotReloadable` modules.

### 4. StartAsync (concurrent)

All `AsyncModule` implementations run concurrently. Each must return quickly — spawn goroutines inside `StartAsync`. If any returns an error, shutdown begins.

### 5. Start (concurrent)

All `SyncModule` implementations run concurrently and are expected to block until the context is cancelled (e.g. a gRPC or HTTP server's listen loop). If any returns an error, shutdown begins.

### 6. Shutdown

On `SIGTERM`, `SIGINT`, or any start error, the runtime calls every module's `Shutdown` in **reverse init order** with a 30-second deadline.

---

## Automatic dependency ordering

Pass `Provider` and `Dependent` to let the runtime determine order for you:

```go
type MyModule struct{}

func (m *MyModule) Provides() []reflect.Type {
    return []reflect.Type{reflect.TypeOf((*MyService)(nil))}
}

func (m *MyModule) Dependencies() (required, optional []reflect.Type) {
    required = []reflect.Type{reflect.TypeOf((*pgxpool.Pool)(nil))}
    optional = []reflect.Type{reflect.TypeOf((*slog.Logger)(nil))}
    return
}
```

- **Required** — must be provided by another module; startup fails if missing
- **Optional** — used if available, silently skipped if not

Modules with no declarations are placed in the queue in their original order relative to each other. A cycle in declared dependencies causes a startup error.
