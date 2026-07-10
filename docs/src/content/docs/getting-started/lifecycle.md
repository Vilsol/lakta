---
title: Module Lifecycle
description: How Lakta initializes, starts, and shuts down modules.
---

<svg viewBox="0 0 780 230" role="img" aria-label="Lifecycle: dependency sort, then sequential init, logger injection, concurrent start; SIGTERM or a start error triggers shutdown in reverse init order with a 30-second deadline." style="max-width: 100%; height: auto;">
  <defs>
    <marker id="lc-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
      <path d="M0 0 L10 5 L0 10 z" fill="var(--sl-color-gray-3)"/>
    </marker>
  </defs>
  <g stroke="var(--sl-color-gray-5)" fill="var(--sl-color-gray-6)">
    <rect x="8" y="24" width="132" height="60" rx="8"/>
    <rect x="168" y="24" width="126" height="60" rx="8"/>
    <rect x="322" y="24" width="134" height="60" rx="8"/>
    <rect x="484" y="24" width="150" height="60" rx="8"/>
    <rect x="662" y="24" width="110" height="60" rx="8"/>
  </g>
  <rect x="280" y="150" width="310" height="60" rx="8" fill="var(--sl-color-accent-low)" stroke="var(--sl-color-accent)"/>
  <g stroke="var(--sl-color-gray-3)" stroke-width="1.5" fill="none" marker-end="url(#lc-arrow)">
    <line x1="142" y1="54" x2="164" y2="54"/>
    <line x1="296" y1="54" x2="318" y2="54"/>
    <line x1="458" y1="54" x2="480" y2="54"/>
    <line x1="636" y1="54" x2="658" y2="54"/>
    <path d="M717 86 V180 H596"/>
  </g>
  <g font-size="13" font-weight="600" fill="var(--sl-color-white)" text-anchor="middle">
    <text x="74" y="50">Dependency sort</text>
    <text x="231" y="50">Init</text>
    <text x="389" y="50">Logger injection</text>
    <text x="559" y="50">Start · StartAsync</text>
    <text x="717" y="58">Running</text>
    <text x="435" y="176">Shutdown</text>
  </g>
  <g font-size="10.5" fill="var(--sl-color-gray-3)" text-anchor="middle">
    <text x="74" y="68">topological (Kahn)</text>
    <text x="231" y="68">sequential · dep order</text>
    <text x="389" y="68">slog → context</text>
    <text x="559" y="68">concurrent</text>
    <text x="435" y="194">reverse init order · 30s deadline</text>
  </g>
  <text x="704" y="136" font-size="11" fill="var(--sl-color-accent-high)" text-anchor="end">SIGTERM · SIGINT · start error</text>
</svg>

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
