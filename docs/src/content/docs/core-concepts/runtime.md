---
title: Runtime
description: How NewRuntime orchestrates module lifecycle and signal handling.
---

`lakta.NewRuntime()` is the entry point for every Lakta service. It wires modules together and manages their full lifecycle.

## Creating a runtime

```go
if err := lakta.NewRuntime(module1, module2, module3, ...).Run(); err != nil {
    os.Exit(1)
}
```

`Run` installs signal handlers for `SIGTERM`/`SIGINT`, then delegates to `RunContext`. Pass your own context to `RunContext` directly if you need custom cancellation.

## What the runtime does

1. **Dependency sort** — topologically sorts all modules from their `Provider`/`Dependent` declarations. Returns an error immediately if a required dependency has no provider or a cycle is detected.
2. **Init** — calls `LoadConfig` then `Init` on each module sequentially in resolved order.
3. **Logger injection** — retrieves `*slog.Logger` from DI and injects it into context. Wires hot-reload callbacks for `HotReloadable` modules.
4. **StartAsync** — all `AsyncModule` implementations start concurrently and must return quickly.
5. **Start** — all `SyncModule` implementations start concurrently and block until shutdown.
6. **Shutdown** — on signal or any start error, calls `Shutdown` on all initialized modules in reverse init order with a 30-second deadline.

## Module ordering

You do not need to pass modules in dependency order. The runtime resolves order automatically from `Provider` and `Dependent` declarations:

```go
// These can be in any order — the runtime sorts them
lakta.NewRuntime(
    myapp.NewModule(),       // declares dependency on *pgxpool.Pool, *slog.Logger
    pgx.NewModule(...),      // declares it provides *pgxpool.Pool
    slog.NewModule(),        // declares it provides *slog.Logger
    config.NewModule(...),   // declares it provides *koanf.Koanf
    tint.NewModule(),        // no deps declared — sorted by original position
)
```

Modules that implement neither `Provider` nor `Dependent` are queued in their original relative order.

## Error handling

| Situation | Behaviour |
|-----------|-----------|
| Required dep has no provider | Error before any `Init` fires |
| Dependency cycle detected | Error before any `Init` fires |
| `Init` returns error | Remaining modules not started; already-initialized modules shut down in reverse order |
| `StartAsync` returns error | Shutdown triggered for all initialized modules |
| `Start` returns error | Shutdown triggered for all initialized modules |
| `Shutdown` returns error | Logged; shutdown continues; first error returned to caller |

## Signal handling

`Run` handles `SIGTERM` and `SIGINT`. A second signal forces immediate exit without waiting for graceful shutdown.
