# Production Lifecycle Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close 7 production-readiness gaps in the lakta runtime and modules — enforced shutdown, panic recovery, sync-exit-triggers-shutdown, gRPC graceful shutdown honoring ctx, generous timeout defaults, telemetry fail-open, DB pool knobs, and hot-reload validation/rollback.

**Architecture:** Three changes land in the runtime core (`pkg/lakta/runtime.go`); the rest are per-module config/lifecycle changes following the existing two-file (`config.go`/`module.go`) and domain-method patterns. Every change ships red-green: a failing test reproducing the gap first, then the minimal fix.

**Tech Stack:** Go 1.26, `testza` assertions, `pkg/testkit` mocks, `conc/pool`, `samber/oops`, `gofiber/fiber/v3`, `google.golang.org/grpc`, `jackc/pgx/v5`, OpenTelemetry SDK, `knadh/koanf/v2`.

**Spec:** `docs/superpowers/specs/2026-06-08-production-lifecycle-hardening-design.md`

**Test commands:** plain `go test` (no cgo needed). Run a single test with `go test ./pkg/<path>/... -run TestName -v`.

**Internal vs external test packages:** Files in `package lakta` (e.g. `runtime_internal_test.go`) **cannot** import `pkg/testkit` (testkit imports `lakta` → cycle). Use local mock types there. Files in `package lakta_test` (e.g. `runtime_test.go`) **can** import testkit.

---

## Task 1: Runtime — enforced shutdown deadline

**Files:**
- Modify: `pkg/lakta/runtime.go` (add `shutdownModule`; rewrite `shutdown` :236-257 and `teardown` :220-233; add imports)
- Test: `pkg/lakta/runtime_internal_test.go` (create, `package lakta`)

- [ ] **Step 1: Write the failing test**

Create `pkg/lakta/runtime_internal_test.go`:

```go
package lakta

import (
	"context"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
)

type blockingShutdownModule struct {
	release chan struct{}
}

func (blockingShutdownModule) Init(context.Context) error     { return nil }
func (m blockingShutdownModule) Shutdown(context.Context) error { <-m.release; return nil }

func TestShutdownModule_ReturnsOnDeadline(t *testing.T) {
	m := blockingShutdownModule{release: make(chan struct{})}
	t.Cleanup(func() { close(m.release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := shutdownModule(ctx, m)

	testza.AssertNotNil(t, err)
	testza.AssertTrue(t, time.Since(start) < time.Second, "shutdownModule must return near the deadline, not block on Shutdown")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/lakta/ -run TestShutdownModule_ReturnsOnDeadline -v`
Expected: FAIL to compile — `undefined: shutdownModule`.

- [ ] **Step 3: Write minimal implementation**

In `pkg/lakta/runtime.go`, add `"runtime/debug"` to the import block. Add this function (place above `teardown`):

```go
// shutdownModule runs module.Shutdown in a goroutine and races it against ctx.
// On deadline expiry the goroutine is abandoned (the process is exiting) and a
// timeout error is returned. Panics inside Shutdown are recovered into an error.
func shutdownModule(ctx context.Context, module Module) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- oops.With("stack", string(debug.Stack())).Errorf("panic during shutdown: %v", r)
			}
		}()
		done <- module.Shutdown(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "shutdown deadline exceeded")
	}
}
```

Rewrite `teardown` (currently :220-233) to bound itself with its own deadline and route through `shutdownModule`:

```go
// teardown shuts down modules in reverse order under a fresh deadline, logging
// but not returning errors. Used when cleaning up after an Init failure.
func (r *Runtime) teardown(ctx context.Context, initialized []Module) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	for _, module := range slices.Backward(initialized) {
		name := fmt.Sprintf("%T", module)

		if timeoutCtx.Err() != nil {
			slox.Error(ctx, "shutdown deadline exceeded, skipping module teardown", slog.String("name", name))
			continue
		}

		if err := shutdownModule(timeoutCtx, module); err != nil {
			slox.Error(ctx, "failed shutting down module", slog.String("name", name), slog.Any("error", err))
		}
	}
}
```

Rewrite `shutdown` (currently :236-257) to skip-on-expiry and route through `shutdownModule`:

```go
// shutdown shuts down modules in reverse order, returning the first error.
// Modules remaining after the deadline expires are logged and skipped.
func (r *Runtime) shutdown(ctx context.Context, initialized []Module) error {
	var firstErr error

	for _, module := range slices.Backward(initialized) {
		name := fmt.Sprintf("%T", module)

		if ctx.Err() != nil {
			slox.Error(ctx, "shutdown deadline exceeded, skipping module", slog.String("name", name))
			if firstErr == nil {
				firstErr = oops.With("name", name).Wrapf(ctx.Err(), "shutdown deadline exceeded")
			}
			continue
		}

		if err := shutdownModule(ctx, module); err != nil {
			slox.Error(ctx, "failed shutting down module", slog.String("name", name), slog.Any("error", err))
			if firstErr == nil {
				firstErr = oops.With("name", name).Wrapf(err, "failed shutting down module")
			}
		}
	}

	return firstErr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/lakta/ -run TestShutdownModule_ReturnsOnDeadline -v`
Expected: PASS. Then `go build ./...` — expected: clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/lakta/runtime.go pkg/lakta/runtime_internal_test.go
git commit -m "feat(lakta): enforce shutdown deadline per module"
```

---

## Task 2: Runtime — panic recovery on lifecycle calls

**Files:**
- Modify: `pkg/lakta/runtime.go` (add `safeCall`; wrap `Init` :76, `StartAsync` :122, `Start` :170)
- Test: `pkg/lakta/runtime_internal_test.go` (append unit test); `pkg/lakta/runtime_test.go` (append integration test, `package lakta_test`)

- [ ] **Step 1: Write the failing tests**

Append to `pkg/lakta/runtime_internal_test.go`:

```go
func TestSafeCall_RecoversPanic(t *testing.T) {
	err := safeCall(func() error { panic("boom") })

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "boom")
}
```

Append to `pkg/lakta/runtime_test.go` (confirm its package is `lakta_test`; if the file does not exist, create it with `package lakta_test`):

```go
func TestRunContext_InitPanicTriggersTeardown(t *testing.T) {
	first := testkit.NewMockModule()
	panicker := testkit.NewMockModule()
	panicker.OnInit = func(context.Context) error { panic("init boom") }

	rt := lakta.NewRuntime(first, panicker)
	err := rt.RunContext(context.Background())

	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, int32(1), first.ShutdownCalls.Load())
}
```

Ensure `runtime_test.go` imports `"context"`, `"testing"`, `"github.com/MarvinJWendt/testza"`, `"github.com/Vilsol/lakta/pkg/lakta"`, `"github.com/Vilsol/lakta/pkg/testkit"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/lakta/ -run 'TestSafeCall_RecoversPanic|TestRunContext_InitPanicTriggersTeardown' -v`
Expected: `TestSafeCall_RecoversPanic` fails to compile (`undefined: safeCall`); `TestRunContext_InitPanicTriggersTeardown` panics/crashes the test binary (unrecovered panic).

- [ ] **Step 3: Write minimal implementation**

In `pkg/lakta/runtime.go`, add `safeCall` (place near `shutdownModule`):

```go
// safeCall runs fn, converting any panic into an error with a stack trace.
func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = oops.With("stack", string(debug.Stack())).Errorf("panic: %v", r)
		}
	}()
	return fn()
}
```

Wrap the Init call (currently :76 `if err := module.Init(ctx); err != nil {`):

```go
		if err := safeCall(func() error { return module.Init(ctx) }); err != nil {
```

Wrap the async start (currently :122 `if err := m.StartAsync(ctx); err != nil {`):

```go
			asyncPool.Go(func(ctx context.Context) error {
				if err := safeCall(func() error { return m.StartAsync(ctx) }); err != nil {
					slox.Error(ctx, "failed starting async module",
						slog.String("name", name), slog.Any("error", err))

					return oops.With("name", name).Wrapf(err, "failed starting module")
				}

				return nil
			})
```

Wrap the sync start (currently :170 `if err := m.Start(ctx); err != nil {`):

```go
				if err := safeCall(func() error { return m.Start(ctx) }); err != nil {
					slox.Error(ctx, "failed starting sync module",
						slog.String("name", name), slog.Any("error", err))

					return oops.With("name", name).Wrapf(err, "failed starting module")
				}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/lakta/ -run 'TestSafeCall_RecoversPanic|TestRunContext_InitPanicTriggersTeardown' -v`
Expected: both PASS, no crash.

- [ ] **Step 5: Commit**

```bash
git add pkg/lakta/runtime.go pkg/lakta/runtime_internal_test.go pkg/lakta/runtime_test.go
git commit -m "feat(lakta): recover panics in module lifecycle calls"
```

---

## Task 3: Runtime — sync-exit triggers shutdown ("first done wins")

**Files:**
- Modify: `pkg/lakta/runtime.go` (sync pool block :152-210; add `"errors"` import)
- Test: `pkg/lakta/runtime_test.go` (append, `package lakta_test`)

- [ ] **Step 1: Write the failing test**

Append to `pkg/lakta/runtime_test.go`:

```go
func TestRunContext_SyncCleanExitTriggersShutdown(t *testing.T) {
	fast := testkit.NewMockSyncModule()         // Start returns nil immediately
	blocker := testkit.NewMockSyncModule()
	blocker.BlockStart = make(chan struct{})    // blocks until ctx is cancelled

	rt := lakta.NewRuntime(fast, blocker)

	done := make(chan error, 1)
	go func() { done <- rt.RunContext(context.Background()) }()

	select {
	case err := <-done:
		testza.AssertNil(t, err)
		testza.AssertEqual(t, int32(1), blocker.ShutdownCalls.Load())
		testza.AssertEqual(t, int32(1), fast.ShutdownCalls.Load())
	case <-time.After(5 * time.Second):
		t.Fatal("RunContext hung — first-done-wins did not cancel the blocking sync module")
	}
}
```

Add `"time"` to the `runtime_test.go` imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/lakta/ -run TestRunContext_SyncCleanExitTriggersShutdown -v`
Expected: FAIL — times out at 5s (`blocker.Start` keeps the sync pool's `Wait()` blocked because a clean exit does not cancel siblings today).

- [ ] **Step 3: Write minimal implementation**

Add `"errors"` to the import block. Replace the sync block (currently :152-210) with:

```go
	// Phase 2: Start sync modules. First sync module to return (clean OR error)
	// cancels syncCtx, so siblings observe cancellation and return — then the
	// runtime proceeds to graceful shutdown.
	syncCtx, cancelSync := context.WithCancel(ctx)
	defer cancelSync()

	syncPool := pool.New().
		WithErrors().
		WithContext(syncCtx).
		WithCancelOnError()

	hasSyncModules := false

	for _, module := range sorted {
		if m, ok := module.(SyncModule); ok {
			hasSyncModules = true
			name := fmt.Sprintf("%T", module)

			syncPool.Go(func(ctx context.Context) error {
				defer cancelSync() // first to return cancels siblings via syncCtx

				if cs, ok := m.(contextSetter); ok {
					cs.setCtx(ctx)
				}

				if err := safeCall(func() error { return m.Start(ctx) }); err != nil {
					slox.Error(ctx, "failed starting sync module",
						slog.String("name", name), slog.Any("error", err))

					return oops.With("name", name).Wrapf(err, "failed starting module")
				}

				return nil
			})
		}
	}

	if hasSyncModules {
		syncDone := make(chan error, 1)
		go func() {
			syncDone <- syncPool.Wait()
		}()

		select {
		case <-ctx.Done():
			slox.Info(ctx, "shutdown signal received")
		case err := <-syncDone:
			// A sync module returned, so we are shutting down regardless. Surface a
			// genuine module failure, but ignore context.Canceled induced by our own
			// cancelSync when a sibling returned cleanly first.
			if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
				slox.Error(ctx, "sync modules failed", slog.Any("error", err))

				shutdownTimeout, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
				defer cancel()

				if shutdownErr := r.shutdown(shutdownTimeout, initialized); shutdownErr != nil {
					return shutdownErr
				}

				return err
			}

			slox.Info(ctx, "a sync module stopped, shutting down")
		}
	} else {
		<-ctx.Done()
		slox.Info(ctx, "shutdown signal received")
	}

	shutdownTimeout, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	return r.shutdown(shutdownTimeout, initialized)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/lakta/ -run TestRunContext_SyncCleanExitTriggersShutdown -v`
Expected: PASS within ~1s. Then `go test ./pkg/lakta/... -v` — expected: all existing runtime tests still pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/lakta/runtime.go pkg/lakta/runtime_test.go
git commit -m "feat(lakta): a sync module exiting triggers graceful shutdown"
```

---

## Task 4: gRPC server — Shutdown honors context deadline

**Files:**
- Modify: `pkg/grpc/server/module.go` (`Shutdown` :160-163)
- Test: `pkg/grpc/server/module_internal_test.go` (append, `package grpcserver`)

- [ ] **Step 1: Write the failing test**

Append to `pkg/grpc/server/module_internal_test.go`. Use grpc's standard health server (full `Watch` support) to hold a long-lived stream so `GracefulStop` would block indefinitely:

```go
func TestShutdown_ForcesStopOnDeadline(t *testing.T) {
	ctx := context.Background()
	m := NewModule(WithHost("127.0.0.1"), WithPort(0))
	testza.AssertNoError(t, m.Init(ctx))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	testza.AssertNoError(t, err)

	hs := grpchealth.NewServer()
	healthpb.RegisterHealthServer(m.server, hs)
	go func() { _ = m.server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNoError(t, err)
	defer func() { _ = conn.Close() }()

	// Open a long-lived Watch stream so GracefulStop would block until it ends.
	stream, err := healthpb.NewHealthClient(conn).Watch(ctx, &healthpb.HealthCheckRequest{})
	testza.AssertNoError(t, err)
	_, _ = stream.Recv() // receive initial status; stream stays open

	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	testza.AssertNoError(t, m.Shutdown(shutdownCtx))
	testza.AssertTrue(t, time.Since(start) < 2*time.Second, "Shutdown must force-stop within the deadline")
}
```

Ensure the test file imports: `"context"`, `"net"`, `"testing"`, `"time"`, `"github.com/MarvinJWendt/testza"`, `"google.golang.org/grpc"`, `"google.golang.org/grpc/credentials/insecure"`, `grpchealth "google.golang.org/grpc/health"`, `healthpb "google.golang.org/grpc/health/grpc_health_v1"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/grpc/server/ -run TestShutdown_ForcesStopOnDeadline -v -timeout 30s`
Expected: FAIL — the test hangs until the 30s test timeout because `GracefulStop()` waits for the open Watch stream and ignores the context.

- [ ] **Step 3: Write minimal implementation**

Replace `Shutdown` (:160-163):

```go
// Shutdown gracefully stops the gRPC server, forcing a stop if the context
// deadline is exceeded before in-flight RPCs drain.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.server == nil {
		return nil
	}

	stopped := make(chan struct{})
	go func() {
		m.server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-ctx.Done():
		m.server.Stop()
	}

	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/grpc/server/ -run TestShutdown_ForcesStopOnDeadline -v -timeout 30s`
Expected: PASS in well under 2s.

- [ ] **Step 5: Commit**

```bash
git add pkg/grpc/server/module.go pkg/grpc/server/module_internal_test.go
git commit -m "fix(grpc): force-stop server when shutdown deadline exceeded"
```

---

## Task 5: HTTP fiber — generous timeout defaults

**Files:**
- Modify: `pkg/http/fiber/config.go` (`ToFiberConfig` :74-90; add `time` import + constants)
- Test: `pkg/http/fiber/module_internal_test.go` (append, `package fiberserver`)

- [ ] **Step 1: Write the failing tests**

Append to `pkg/http/fiber/module_internal_test.go`:

```go
func TestToFiberConfig_AppliesGenerousTimeoutDefaults(t *testing.T) {
	cfg := NewConfig().ToFiberConfig()

	testza.AssertEqual(t, 30*time.Second, cfg.ReadTimeout)
	testza.AssertEqual(t, 60*time.Second, cfg.WriteTimeout)
	testza.AssertEqual(t, 120*time.Second, cfg.IdleTimeout)
}

func TestToFiberConfig_UserDefaultsOverrideTimeouts(t *testing.T) {
	cfg := NewConfig(WithDefaults(fiber.Config{ReadTimeout: 5 * time.Second})).ToFiberConfig()

	testza.AssertEqual(t, 5*time.Second, cfg.ReadTimeout)        // user value preserved
	testza.AssertEqual(t, 60*time.Second, cfg.WriteTimeout)      // unset → default
}
```

Ensure imports include `"testing"`, `"time"`, `"github.com/MarvinJWendt/testza"`, `"github.com/gofiber/fiber/v3"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/http/fiber/ -run TestToFiberConfig_ -v`
Expected: FAIL — timeouts are `0` (no defaults applied).

- [ ] **Step 3: Write minimal implementation**

In `pkg/http/fiber/config.go`, add `"time"` to imports and these constants below the existing `const` block (:13-16):

```go
const (
	defaultReadTimeout  = 30 * time.Second
	defaultWriteTimeout = 60 * time.Second
	defaultIdleTimeout  = 120 * time.Second
)
```

In `ToFiberConfig` (:74-90), before `return cfg`, apply zero-value-guarded defaults:

```go
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = defaultReadTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}

	return cfg
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/http/fiber/ -run TestToFiberConfig_ -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/http/fiber/config.go pkg/http/fiber/module_internal_test.go
git commit -m "feat(http): apply generous read/write/idle timeout defaults"
```

---

## Task 6: gRPC server — keepalive defaults via domain methods

**Files:**
- Modify: `pkg/grpc/server/config.go` (add constants + `KeepaliveServerParameters`/`KeepaliveEnforcementPolicy` methods); `pkg/grpc/server/module.go` (`grpc.NewServer` opts :84-96)
- Test: `pkg/grpc/server/module_internal_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `pkg/grpc/server/module_internal_test.go`:

```go
func TestKeepaliveDefaults(t *testing.T) {
	c := NewConfig()

	sp := c.KeepaliveServerParameters()
	testza.AssertEqual(t, 5*time.Minute, sp.MaxConnectionIdle)
	testza.AssertEqual(t, 2*time.Hour, sp.Time)
	testza.AssertEqual(t, 20*time.Second, sp.Timeout)

	ep := c.KeepaliveEnforcementPolicy()
	testza.AssertEqual(t, 30*time.Second, ep.MinTime)
	testza.AssertTrue(t, ep.PermitWithoutStream)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/grpc/server/ -run TestKeepaliveDefaults -v`
Expected: FAIL to compile — methods undefined.

- [ ] **Step 3: Write minimal implementation**

In `pkg/grpc/server/config.go`, add `"time"` and `"google.golang.org/grpc/keepalive"` imports, these constants:

```go
const (
	defaultMaxConnectionIdle = 5 * time.Minute
	defaultKeepaliveTime     = 2 * time.Hour
	defaultKeepaliveTimeout  = 20 * time.Second
	defaultEnforcementMinTime = 30 * time.Second
)
```

and these domain methods:

```go
// KeepaliveServerParameters returns generous keepalive parameters for the server.
func (c *Config) KeepaliveServerParameters() keepalive.ServerParameters {
	return keepalive.ServerParameters{
		MaxConnectionIdle: defaultMaxConnectionIdle,
		Time:              defaultKeepaliveTime,
		Timeout:           defaultKeepaliveTimeout,
	}
}

// KeepaliveEnforcementPolicy returns the server's keepalive enforcement policy.
func (c *Config) KeepaliveEnforcementPolicy() keepalive.EnforcementPolicy {
	return keepalive.EnforcementPolicy{
		MinTime:             defaultEnforcementMinTime,
		PermitWithoutStream: true,
	}
}
```

In `pkg/grpc/server/module.go`, add `"google.golang.org/grpc/keepalive"` to imports, and add two options to the `grpc.NewServer(...)` call (:84-96), after `grpc.StatsHandler(...)`:

```go
		grpc.KeepaliveParams(m.config.KeepaliveServerParameters()),
		grpc.KeepaliveEnforcementPolicy(m.config.KeepaliveEnforcementPolicy()),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/grpc/server/ -run TestKeepaliveDefaults -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add pkg/grpc/server/config.go pkg/grpc/server/module.go pkg/grpc/server/module_internal_test.go
git commit -m "feat(grpc): apply server keepalive + enforcement defaults"
```

---

## Task 7: gRPC client — keepalive defaults

**Files:**
- Modify: `pkg/grpc/client/config.go` (add constants + `KeepaliveParams` method; `DialOptions` :73-81)
- Test: `pkg/grpc/client/module_internal_test.go` (append, `package grpcclient`)

- [ ] **Step 1: Write the failing test**

Append to `pkg/grpc/client/module_internal_test.go`:

```go
func TestClientKeepaliveDefaults(t *testing.T) {
	c := NewConfig()

	kp := c.KeepaliveParams()
	testza.AssertEqual(t, 30*time.Second, kp.Time)
	testza.AssertEqual(t, 20*time.Second, kp.Timeout)
}
```

Ensure imports include `"testing"`, `"time"`, `"github.com/MarvinJWendt/testza"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/grpc/client/ -run TestClientKeepaliveDefaults -v`
Expected: FAIL to compile — `KeepaliveParams` undefined.

- [ ] **Step 3: Write minimal implementation**

In `pkg/grpc/client/config.go`, add `"time"` and `"google.golang.org/grpc/keepalive"` imports, these constants:

```go
const (
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 20 * time.Second
)
```

this method:

```go
// KeepaliveParams returns generous client keepalive parameters.
func (c *Config) KeepaliveParams() keepalive.ClientParameters {
	return keepalive.ClientParameters{
		Time:                defaultKeepaliveTime,
		Timeout:             defaultKeepaliveTimeout,
		PermitWithoutStream: false,
	}
}
```

and append the dial option in `DialOptions` (:73-81), inside the initial `opts` slice:

```go
	opts := []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithKeepaliveParams(c.KeepaliveParams()),
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/grpc/client/ -run TestClientKeepaliveDefaults -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add pkg/grpc/client/config.go pkg/grpc/client/module_internal_test.go
git commit -m "feat(grpc): apply client keepalive defaults"
```

---

## Task 8: pgx — pool lifetime/idle/health knobs + statement_timeout

**Files:**
- Modify: `pkg/db/drivers/pgx/config.go` (new fields, defaults, options, `NewPoolConfig` :87-105)
- Test: `pkg/db/drivers/pgx/module_internal_test.go` (append, `package pgx`)

- [ ] **Step 1: Write the failing tests**

Append to `pkg/db/drivers/pgx/module_internal_test.go`:

```go
func TestNewPoolConfig_AppliesPoolDefaults(t *testing.T) {
	c := NewConfig(WithDSN("postgres://u:p@localhost:5432/db"))
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNoError(t, err)

	testza.AssertEqual(t, time.Hour, pc.MaxConnLifetime)
	testza.AssertEqual(t, 30*time.Minute, pc.MaxConnIdleTime)
	testza.AssertEqual(t, time.Minute, pc.HealthCheckPeriod)
	testza.AssertEqual(t, "30000", pc.ConnConfig.RuntimeParams["statement_timeout"])
}

func TestNewPoolConfig_StatementTimeoutDisabledWhenZero(t *testing.T) {
	c := NewConfig(WithDSN("postgres://u:p@localhost:5432/db"), WithStatementTimeout(0))
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNoError(t, err)

	_, ok := pc.ConnConfig.RuntimeParams["statement_timeout"]
	testza.AssertFalse(t, ok)
}
```

Ensure imports include `"testing"`, `"time"`, `"github.com/MarvinJWendt/testza"`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/db/drivers/pgx/ -run TestNewPoolConfig_ -v`
Expected: FAIL to compile — `WithStatementTimeout` undefined and fields unset.

- [ ] **Step 3: Write minimal implementation**

In `pkg/db/drivers/pgx/config.go`, add `"strconv"` and `"time"` to imports. Add fields to the `Config` struct (after `HealthCheck` :34):

```go
	// MinConns is the minimum number of idle connections kept in the pool.
	MinConns int32 `koanf:"min_conns"`

	// MaxConnLifetime is the maximum age of a connection before it is closed.
	MaxConnLifetime time.Duration `koanf:"max_conn_lifetime"`

	// MaxConnIdleTime is the maximum idle time before a connection is closed.
	MaxConnIdleTime time.Duration `koanf:"max_conn_idle_time"`

	// HealthCheckPeriod is how often the pool checks idle connection health.
	HealthCheckPeriod time.Duration `koanf:"health_check_period"`

	// StatementTimeout sets the per-statement timeout (Postgres statement_timeout). Zero disables it.
	StatementTimeout time.Duration `koanf:"statement_timeout"`
```

Add the new defaults in `NewDefaultConfig` (:41-49), inside the returned struct literal:

```go
		MinConns:          0,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
		StatementTimeout:  30 * time.Second,
```

In `NewPoolConfig` (:87-105), after `poolConfig.MaxConns = c.MaxOpenConns` (:93), add:

```go
	poolConfig.MinConns = c.MinConns
	poolConfig.MaxConnLifetime = c.MaxConnLifetime
	poolConfig.MaxConnIdleTime = c.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = c.HealthCheckPeriod

	if c.StatementTimeout > 0 {
		if poolConfig.ConnConfig.RuntimeParams == nil {
			poolConfig.ConnConfig.RuntimeParams = map[string]string{}
		}
		poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(c.StatementTimeout.Milliseconds(), 10)
	}
```

Add the options at the end of `config.go`:

```go
// WithMinConns sets the minimum number of idle pool connections.
func WithMinConns(n int32) Option {
	return func(m *Config) { m.MinConns = n }
}

// WithMaxConnLifetime sets the maximum connection age.
func WithMaxConnLifetime(d time.Duration) Option {
	return func(m *Config) { m.MaxConnLifetime = d }
}

// WithMaxConnIdleTime sets the maximum connection idle time.
func WithMaxConnIdleTime(d time.Duration) Option {
	return func(m *Config) { m.MaxConnIdleTime = d }
}

// WithHealthCheckPeriod sets the pool health-check interval.
func WithHealthCheckPeriod(d time.Duration) Option {
	return func(m *Config) { m.HealthCheckPeriod = d }
}

// WithStatementTimeout sets the Postgres statement_timeout. Zero disables it.
func WithStatementTimeout(d time.Duration) Option {
	return func(m *Config) { m.StatementTimeout = d }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/db/drivers/pgx/ -run TestNewPoolConfig_ -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/db/drivers/pgx/config.go pkg/db/drivers/pgx/module_internal_test.go
git commit -m "feat(pgx): add pool lifetime/idle/health + statement_timeout knobs"
```

---

## Task 9: otel — fail-open with strict `required` flag

**Files:**
- Modify: `pkg/otel/config.go` (`Required` field, default, `WithRequired`); `pkg/otel/module.go` (`Init` :52-96, add `handleSetupError`); `pkg/otel/setup.go` (`buildResource` :108-133 swallows resource errors)
- Test: `pkg/otel/module_internal_test.go` (append, `package otel`)

- [ ] **Step 1: Write the failing tests**

Append to `pkg/otel/module_internal_test.go`:

```go
func TestInit_FailOpenWhenSetupErrorsAndNotRequired(t *testing.T) {
	h := testkit.NewHarness(t)
	m := NewModule(
		WithRequired(false),
		WithSetupFn(func(context.Context, string) (func(context.Context) error, error) {
			return nil, errors.New("collector exploded")
		}),
	)

	testza.AssertNoError(t, m.Init(h.Ctx()))
}

func TestInit_FatalWhenSetupErrorsAndRequired(t *testing.T) {
	h := testkit.NewHarness(t)
	m := NewModule(
		WithRequired(true),
		WithSetupFn(func(context.Context, string) (func(context.Context) error, error) {
			return nil, errors.New("collector exploded")
		}),
	)

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}
```

Ensure imports include `"context"`, `"errors"`, `"testing"`, `"github.com/MarvinJWendt/testza"`, `"github.com/Vilsol/lakta/pkg/testkit"`. (`module_internal_test.go` is `package otel`; importing testkit is fine — testkit imports `lakta`, not `otel`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/otel/ -run 'TestInit_FailOpen|TestInit_Fatal' -v`
Expected: FAIL to compile — `WithRequired` undefined; and the fail-open test would fail because today the `SetupFn` error path returns the error unconditionally (:63-65).

- [ ] **Step 3: Write minimal implementation**

In `pkg/otel/config.go`, add a field (after `Enabled` :67):

```go
	// Required makes telemetry setup failures fatal. When false (default), setup
	// failures are logged and the app continues with noop providers.
	Required bool `koanf:"required"`
```

Add the default in `NewDefaultConfig` (:89-101): `Required: false,` (explicit). Add the option:

```go
// WithRequired makes telemetry setup failures fatal instead of fail-open.
func WithRequired(required bool) Option {
	return func(m *Config) { m.Required = required }
}
```

In `pkg/otel/module.go`, add `"log/slog"` and `"github.com/Vilsol/slox"` imports. Replace the two fatal `return err` sites in `Init` with `handleSetupError`, and add the helper.

Replace the `SetupFn` error branch (:62-65):

```go
		shutdown, err := m.config.SetupFn(ctx, m.config.ServiceName)
		if err != nil {
			return m.handleSetupError(ctx, err)
		}
```

Replace the `setupOTelSDK` error branch (:74-77):

```go
	m.providers, err = setupOTelSDK(ctx, m.config)
	if err != nil {
		return m.handleSetupError(ctx, err)
	}
```

Add the helper (place after `Init`):

```go
// handleSetupError applies the fail-open policy: fatal when Required, otherwise
// log a warning, register noop providers, and continue.
func (m *Module) handleSetupError(ctx context.Context, err error) error {
	if m.config.Required {
		return oops.Wrapf(err, "failed to setup OpenTelemetry SDK")
	}

	slox.Warn(ctx, "OpenTelemetry setup failed, continuing without telemetry", slog.Any("error", err))

	m.providers = otelProviders{shutdown: func(context.Context) error { return nil }}
	lakta.ProvideValue[oteltrace.TracerProvider](ctx, nooptrace.NewTracerProvider())
	lakta.ProvideValue[otelmetric.MeterProvider](ctx, noopmetric.NewMeterProvider())
	lakta.ProvideValue[otellog.LoggerProvider](ctx, nooplog.NewLoggerProvider())

	return nil
}
```

In `pkg/otel/setup.go`, make `buildResource` (:108-133) never fail on a partial resource error — `resource.New` returns a usable partial resource even on error (schema-URL conflicts and detector warnings are benign):

```go
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		slog.Default().Warn("partial OpenTelemetry resource, continuing", slog.Any("error", err))
	}
	if res == nil {
		res = resource.Default()
	}

	return res, nil
```

(`slog` is already imported in setup.go.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/otel/ -run 'TestInit_FailOpen|TestInit_Fatal' -v && go test ./pkg/otel/... -v`
Expected: new tests PASS; existing otel tests still pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/otel/config.go pkg/otel/module.go pkg/otel/setup.go pkg/otel/module_internal_test.go
git commit -m "feat(otel): fail-open on setup errors with strict required flag"
```

---

## Task 10: config — hot-reload validation hook + callback panic isolation

**Files:**
- Modify: `pkg/lakta/configurable.go` (`OnValidate` on `ReloadNotifier`; new `ValidatableModule`); `pkg/lakta/runtime.go` (wire validators :88-95); `pkg/config/module.go` (`onValidate` field, `OnValidate`, `reload` :170-201, `safeCallback`); `pkg/testkit/harness.go` (`OnValidate` on test double :85-99)
- Test: `pkg/config/module_internal_test.go` (append, `package config`)

- [ ] **Step 1: Write the failing tests**

Create `pkg/config/module_internal_test.go` (`package config`; `module_test.go` is already `package config`, so this lives alongside it):

```go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"
)

func writeReloadFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReload_ValidatorVetoKeepsOldConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeReloadFile(t, path, "foo: original\n")

	m := NewModule()
	m.configFiles = []configFile{{path: path, parser: yaml.Parser()}}
	testza.AssertNoError(t, m.reload())
	testza.AssertEqual(t, "original", m.Koanf().String("foo"))

	writeReloadFile(t, path, "foo: changed\n")
	m.OnValidate(func(*koanf.Koanf) error { return errors.New("veto") })

	err := m.reload()
	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, "original", m.Koanf().String("foo")) // unchanged after veto
}

func TestReload_CallbackPanicIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeReloadFile(t, path, "foo: v1\n")

	m := NewModule()
	m.configFiles = []configFile{{path: path, parser: yaml.Parser()}}
	testza.AssertNoError(t, m.reload())

	ran := false
	m.OnReload(func(*koanf.Koanf) { panic("callback boom") })
	m.OnReload(func(*koanf.Koanf) { ran = true })

	writeReloadFile(t, path, "foo: v2\n")
	testza.AssertNoError(t, m.reload())
	testza.AssertTrue(t, ran, "later callback must still run after an earlier one panics")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/config/ -run TestReload_ -v`
Expected: FAIL to compile — `OnValidate` undefined on the config module.

- [ ] **Step 3: Write minimal implementation**

In `pkg/lakta/configurable.go`, extend `ReloadNotifier` and add `ValidatableModule`:

```go
// ReloadNotifier can register callbacks invoked after config hot-reload, and
// validators invoked before a reload is committed.
type ReloadNotifier interface {
	OnReload(fn func(k *koanf.Koanf))
	OnValidate(fn func(k *koanf.Koanf) error)
}

// ValidatableModule can veto a config hot-reload before it is committed.
// A non-nil error aborts the reload; the previous config stays live.
type ValidatableModule interface {
	ValidateReload(k *koanf.Koanf) error
}
```

In `pkg/lakta/runtime.go`, extend the hot-reload wiring block (:88-95):

```go
	if notifier, err := do.Invoke[ReloadNotifier](injector); err == nil {
		for _, module := range initialized {
			if hr, ok := module.(HotReloadable); ok {
				notifier.OnReload(hr.OnReload)
			}
			if v, ok := module.(ValidatableModule); ok {
				notifier.OnValidate(v.ValidateReload)
			}
		}
	}
```

In `pkg/config/module.go`, add the field to the `Module` struct (after `onReload` :36):

```go
	onValidate []func(k *koanf.Koanf) error
```

Add the `OnValidate` method (next to `OnReload` :164):

```go
// OnValidate registers a validator invoked on the candidate koanf before a
// reload is committed. A non-nil error aborts the reload.
func (m *Module) OnValidate(fn func(k *koanf.Koanf) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onValidate = append(m.onValidate, fn)
}
```

Rewrite `reload` (:170-201) to gate on validators before committing and isolate callback panics:

```go
func (m *Module) reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newKoanf := koanf.New(".")

	for _, cf := range m.configFiles {
		if err := newKoanf.Load(file.Provider(cf.path), cf.parser); err != nil {
			return oops.Wrapf(err, "failed to reload config file: %s", cf.path)
		}
	}

	if err := newKoanf.Load(env.Provider(m.config.EnvPrefix, ".", func(s string) string {
		return envKeyTransform(m.config.EnvPrefix, s)
	}), nil); err != nil {
		return oops.Wrapf(err, "failed to reload env vars")
	}

	if m.flagSet != nil {
		if err := newKoanf.Load(posflag.Provider(m.flagSet, ".", newKoanf), nil); err != nil {
			return oops.Wrapf(err, "failed to reload CLI flags")
		}
	}

	// Validation gate — any veto aborts the reload before the old config is replaced.
	for _, validate := range m.onValidate {
		if err := validate(newKoanf); err != nil {
			return oops.Wrapf(err, "config reload rejected by validator")
		}
	}

	m.koanf = newKoanf

	for _, fn := range m.onReload {
		m.safeCallback(fn, newKoanf)
	}

	return nil
}

// safeCallback runs a reload callback, isolating panics so one bad module does
// not crash the process or prevent other callbacks from running.
func (m *Module) safeCallback(fn func(k *koanf.Koanf), k *koanf.Koanf) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("config reload callback panicked", slog.Any("panic", r))
		}
	}()
	fn(k)
}
```

Add `"log/slog"` to `pkg/config/module.go` imports.

In `pkg/testkit/harness.go`, add `OnValidate` to the test double so it still satisfies `config.ReloadNotifier` (after `OnReload` :90-92):

```go
// OnValidate implements config.ReloadNotifier (no-op for the test double).
func (r *ReloadNotifier) OnValidate(_ func(*koanf.Koanf) error) {}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/config/ -run TestReload_ -v && go build ./... && go test ./pkg/testkit/... -v`
Expected: new tests PASS; build clean; testkit still compiles/passes.

- [ ] **Step 5: Commit**

```bash
git add pkg/lakta/configurable.go pkg/lakta/runtime.go pkg/config/module.go pkg/testkit/harness.go pkg/config/module_internal_test.go
git commit -m "feat(config): validate hot-reload before commit; isolate callback panics"
```

---

## Final verification

- [ ] **Run the full suite + lint**

```bash
go test ./...
mise exec -- golangci-lint run
```

Expected: all tests pass; lint clean. Fix any `mnd` (magic-number) or `ireturn` findings per repo conventions (named constants; `//nolint:ireturn` on methods returning interfaces).

- [ ] **Update docs**

Add one-line override/streaming caveats to the affected `doc.go` files (`pkg/http/fiber/doc.go`, `pkg/grpc/server/doc.go`, `pkg/grpc/client/doc.go`, `pkg/db/drivers/pgx/doc.go`, `pkg/otel/doc.go`): note the new generous timeout/keepalive defaults and how to override them (`WithDefaults`, config knobs, `WithStatementTimeout`, `WithRequired`). Commit:

```bash
git commit -am "docs: document new timeout/keepalive/telemetry defaults and overrides"
```

---

## Notes / accepted limitations

- gRPC keepalive defaults (Tasks 6/7) are framework constants, not koanf-configurable knobs — overriding them needs a future `WithServerOptions`/config field, out of scope here.
- pgxpool has no pool-level acquire-timeout knob; acquisition is governed by the caller's context deadline (per spec).
- Config reload load-atomicity already exists in the current `reload()` (it commits `m.koanf` only after all loads succeed); Task 10 adds the validation gate and callback isolation on top.
