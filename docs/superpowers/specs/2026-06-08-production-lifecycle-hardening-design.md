# Production Lifecycle Hardening — Design

**Date:** 2026-06-08
**Status:** Approved
**Scope:** One spec, 7 work items. All target failure/lifecycle edges in the runtime
and per-module config. Each item is independently testable and ships red-green (failing
test reproducing the gap first, then the fix).

## Background

A production-readiness pass surfaced gaps concentrated in the runtime's failure and
lifecycle edges (not its architecture, which is sound: DI, topo-sorted init, reverse
teardown, default tracing/recovery on handlers). The blockers and high-priority issues:

1. Shutdown timeout is unenforced — `runtime.shutdown()` calls each `module.Shutdown(ctx)`
   synchronously and never races the deadline; gRPC's `GracefulStop()` ignores `ctx`
   entirely. One stuck module hangs teardown forever.
2. No default network/query timeouts (HTTP, gRPC, DB).
3. No panic recovery during `Init` (and `conc` re-panics on `Start`/`StartAsync`, still
   crashing) — a startup panic crashes without teardown, leaking resources.
4. Telemetry init treats benign `resource.New` partial errors as fatal → cascades to
   whole-app startup failure.
5. DB pool exposes only `MaxConns` — no lifetime/idle/health knobs.
6. Hot-reload swaps in new config and fires callbacks unconditionally — no
   validation, no rollback.
7. Silent partial outage — a sync module's `Start()` returning `nil` doesn't cancel
   siblings; the process stays "up" with a dead server.

## Cross-cutting decisions

- **Defaults policy:** safe defaults, opt-out. Pre-1.0 (v0.1.3) license to change
  behavior; new hardened defaults ship ON, overridable via config.
- **Timeouts:** all-on with generous values; streaming/long-running endpoints override.
- **Telemetry:** fail-open by default, with a `required` flag restoring fatal behavior.

---

## Work item 1 — Runtime lifecycle hardening (`pkg/lakta/runtime.go`)

### 1a. Enforced shutdown deadline

New helper:

```go
// shutdownModule runs module.Shutdown in a goroutine and races it against ctx.
// On deadline expiry the goroutine is abandoned (process is exiting) and the
// timeout error is returned so the caller can record it.
func shutdownModule(ctx context.Context, module Module) error {
    done := make(chan error, 1)
    go func() { done <- module.Shutdown(ctx) }()
    select {
    case err := <-done:
        return err
    case <-ctx.Done():
        return fmt.Errorf("shutdown deadline exceeded: %w", ctx.Err())
    }
}
```

`shutdown()` and `teardown()` call `shutdownModule` instead of `module.Shutdown`
directly. The existing 30s `context.WithTimeout` (runtime.go:212) stays a **shared
total budget** across the reverse-order loop — once it expires, each remaining
module's call returns immediately (ctx already done) and is **explicitly logged as
skipped by name** (not silently dropped). First-error semantics preserved in
`shutdown()`.

**Accepted tradeoff:** skipping remaining modules on deadline expiry sacrifices full
reverse-order teardown for a bounded shutdown. This is the correct tradeoff for a
deadline guarantee — you cannot both honor a hard deadline and guarantee every
module's `Shutdown` runs. The per-module skip logs make any dropped cleanup visible.

**Init-failure teardown also gets a deadline.** Today `teardown()` on an `Init`
failure (runtime.go:67, :78) runs with the unbounded init ctx — a hanging `Shutdown`
there would hang forever, contradicting 1a. Fix: `teardown()` creates its own
`context.WithTimeout(context.Background(), DefaultShutdownTimeout)` and routes every
call through `shutdownModule`, so the same enforced deadline applies on the
init-failure path.

### 1b. Panic recovery on all lifecycle calls

A `recover()` wrapper converts panics (with stack) into `oops` errors:

```go
func safeCall(fn func() error) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = oops.With("stack", string(debug.Stack())).Errorf("panic: %v", r)
        }
    }()
    return fn()
}
```

- `Init` panic → error → triggers reverse `teardown()` (no bare crash, no leak).
- `Start`/`StartAsync` panic inside the pool func → `safeCall` converts it to an error
  **before `conc` ever observes a panic**, so it flows through the normal error path
  regardless of how `conc` handles panics internally → graceful shutdown.
- `Shutdown` panic → recovered (folded into `shutdownModule`'s goroutine), logged,
  doesn't abort teardown of remaining modules.

### 1c. Sync-exit triggers shutdown ("first done wins")

The fix feeds a cancelable child context **into the conc pool itself** so cancellation
propagates through conc's own machinery — rather than layering a second context beside
the pool's provided one (which races conc's `WithCancelOnError` cancel against a manual
one). conc only cancels on *error* (`WithCancelOnError`); we additionally want
cancel-on-*clean-return*.

```go
syncCtx, cancelSync := context.WithCancel(ctx)
defer cancelSync()

syncPool := pool.New().
    WithErrors().
    WithContext(syncCtx).   // pool derives its per-goroutine ctx from syncCtx
    WithCancelOnError()

// in each pool.Go (ctx here is the pool-provided ctx, derived from syncCtx):
syncPool.Go(func(ctx context.Context) error {
    defer cancelSync()      // first to return (nil OR err) cancels syncCtx → pool ctx
    if cs, ok := m.(contextSetter); ok {
        cs.setCtx(ctx)      // contextSetter (runtime.go:166) gets the pool-derived ctx
    }
    return safeCall(func() error { return m.Start(ctx) })
})
```

First sync module to return calls `cancelSync()` → `syncCtx` cancels → the pool-derived
ctx every sibling holds cancels → siblings observe it and return from `Start` →
`syncPool.Wait()` unblocks → the existing `select` at runtime.go:188 fires on `syncDone`
→ runtime proceeds to graceful shutdown. **No new select arm needed** — propagating
through the pool's own context means `Wait()` completes promptly, and waiting for
siblings to finish their `Start` cleanly (rather than abandoning them via a `syncCtx`
select arm) is the desired behavior. SIGTERM via the parent `ctx` still works because
`syncCtx` derives from it.

---

## Work item 2 — gRPC server graceful shutdown (`pkg/grpc/server/module.go`)

`Shutdown(_ context.Context)` → `Shutdown(ctx context.Context)`:

```go
func (m *Module) Shutdown(ctx context.Context) error {
    stopped := make(chan struct{})
    go func() { m.server.GracefulStop(); close(stopped) }()
    select {
    case <-stopped:
    case <-ctx.Done():
        m.server.Stop() // force-close; deadline exceeded
    }
    return nil
}
```

Honors the deadline the runtime now enforces (belt-and-suspenders with 1a).

---

## Work item 3 — Timeout defaults (all-on, generous)

Applied only when the user left the field zero-valued. Overrides + streaming caveat
documented in each module's `doc.go`.

| Module | Defaults |
|---|---|
| HTTP (fiber) | `ReadTimeout 30s`, `WriteTimeout 60s`, `IdleTimeout 120s` (BodyLimit already 4MB) |
| gRPC server | keepalive `ServerParameters{MaxConnectionIdle 5m, Time 2h, Timeout 20s}`, `EnforcementPolicy{MinTime 30s, PermitWithoutStream true}` |
| gRPC client | keepalive `ClientParameters{Time 30s, Timeout 20s}` |
| DB (pgx) | `MaxConnLifetime 1h`, `MaxConnIdleTime 30m`, `HealthCheckPeriod 1m`, `statement_timeout 30s` (runtime param) |

**Honesty note:** pgxpool has no pool-level "acquire timeout" knob — acquisition is
governed by the caller's context deadline. Dropped from scope rather than faked; the
`statement_timeout` + lifetime knobs are the real DB protections.

---

## Work item 4 — Telemetry fail-open + strict flag (`pkg/otel/`)

- New config `required bool` (koanf `required`, default `false`).
- `resource.New` partial errors: if `!required`, log warn + use the partial resource
  it returns anyway; if `required`, fatal.
- Each provider build (tracer/meter/logger) wrapped: `!required` → log warn + skip that
  signal (noop provider); `required` → fatal.
- App always boots unless `telemetry.required: true`.

---

## Work item 5 — DB pool knobs (`pkg/db/drivers/pgx/config.go`)

Additive config fields feeding `pgxpool.Config` (`MaxConns` already exists):
`MinConns`, `MaxConnLifetime`, `MaxConnIdleTime`, `HealthCheckPeriod`. Defaults from
the §3 table. `statement_timeout` applied via `connConfig.RuntimeParams`.

---

## Work item 6 — Hot-reload rollback + validation (`pkg/config/`)

- **Atomic load:** parse the new config into a temporary koanf; on parse/load error →
  keep the old config, log, fire nothing.
- **Validation hook:** new interface

  ```go
  type ValidatableModule interface {
      ValidateReload(k *koanf.Koanf) error
  }
  ```

  The runtime registers validators the same way it wires `HotReloadable`
  (runtime.go:88-95), via a new `OnValidate(fn)` on the `ReloadNotifier`. Reload
  sequence: load temp → run all validators → **any veto aborts** (old config stays
  live, vetoing module logged) → else commit under the write lock → fire `OnReload`.
- **Callback isolation:** each `OnReload` callback wrapped in `recover()` so one
  module's panic can't crash the process mid-reload (aligns with 1b).

---

## Work item 7 — Testing

Red-green per item, `testza` assertions + `pkg/testkit` mocks:

- Blocking-`Shutdown` mock → `shutdown()` returns within the deadline.
- Blocking-`Shutdown` mock on an *Init-failure* path (a later module's `Init` errors,
  an earlier module's `Shutdown` blocks) → `teardown()` returns within the deadline.
- Panicking-`Init` mock → teardown runs, error returned, no crash.
- Clean-exit `MockSyncModule` → runtime proceeds to shutdown, siblings cancelled.
- gRPC `Shutdown` with an already-expired ctx → returns promptly (`Stop()` path).
- Config-assert tests for each timeout default (set + zero-value override).
- otel: forced `resource.New`/exporter error with `required:false` → `Init` returns nil;
  with `required:true` → `Init` errors.
- Hot-reload: vetoing validator and an unparseable file → old config retained.

## Out of scope

- TLS for HTTP/gRPC servers (manual override remains).
- Liveness/readiness probe split and automatic dependency health registration.
- Rate limiting / max-concurrent-streams defaults.
- Retry/backoff on transient DB failures.
