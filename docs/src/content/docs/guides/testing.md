---
title: Testing with testkit
description: Unit and integration testing using Harness, RuntimeHarness, and mock modules.
---

`pkg/testkit` provides test infrastructure so you can test modules without spinning up a full service.

## Harness — unit testing a module

`NewHarness` creates a context with a DI injector, suitable for testing a single module's `Init` in isolation.

```go
func TestMyModule_Init(t *testing.T) {
    h := testkit.NewHarness(t)

    // Provide config if the module is Configurable
    h = h.WithData(map[string]any{
        "modules.mymodule.default.host": "localhost",
        "modules.mymodule.default.port": 8080,
    })

    // Provide any DI dependencies the module needs
    testkit.WithProvider[*pgxpool.Pool](h, func(do.Injector) (*pgxpool.Pool, error) {
        return fakePool, nil
    })

    m := mymodule.NewModule()
    testza.AssertNoError(t, m.Init(h.Ctx()))

    // Invoke what the module registered
    svc := do.MustInvoke[*mymodule.MyService](lakta.GetInjector(h.Ctx()))
    testza.AssertNotNil(t, svc)
}
```

## RuntimeHarness — integration testing

`NewRuntimeHarness` starts a full `Runtime` in a goroutine and gives you a `Shutdown()` to stop it cleanly.

```go
func TestMyModule_Start(t *testing.T) {
    rh := testkit.NewRuntimeHarness(t,
        config.NewModule(),
        slog.NewModule(),
        mymodule.NewModule(),
    )
    defer rh.Shutdown()

    // runtime is now running; test behavior
    // e.g. make an HTTP request, invoke a gRPC method
}
```

## Mock modules

Use mock modules to satisfy the runtime when you don't need real implementations:

```go
mock := testkit.NewMockModule()          // basic Module
syncMock := testkit.NewMockSyncModule()  // SyncModule, has BlockStart chan
asyncMock := testkit.NewMockAsyncModule()
```

`MockSyncModule.BlockStart` is a `chan struct{}` — close it to unblock `Start`:

```go
go func() {
    time.Sleep(100 * time.Millisecond)
    close(syncMock.BlockStart)
}()
```

## Hot-reload testing

```go
h := testkit.NewHarness(t).WithData(initialData)
notifier := do.MustInvoke[*config.ReloadNotifier](lakta.GetInjector(h.Ctx()))
notifier.FireReload(newKoanf)  // triggers all registered OnReload callbacks
```

## Assertions

Always use `testza`:

```go
testza.AssertNoError(t, err)
testza.AssertNotNil(t, result)
testza.AssertEqual(t, expected, actual)
```
