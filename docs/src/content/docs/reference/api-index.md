---
title: API Index
description: Quick reference for key types, functions, and interfaces exported by Lakta.
---

## pkg/lakta

| Symbol | Description |
|--------|-------------|
| `NewRuntime(modules ...Module) *Runtime` | Create and run the service |
| `Runtime.Run()` | Start the runtime, block until shutdown |
| `Module` | Interface: `Init(ctx) error`, `Shutdown(ctx) error` |
| `SyncModule` | Adds `Start(ctx) error` |
| `AsyncModule` | Adds `StartAsync(ctx) error` |
| `Configurable` | Adds `ConfigPath() string`, `LoadConfig(*koanf.Koanf) error` |
| `NamedModule` | Adds `Name() string` |
| `NamedBase` | Embed to satisfy `NamedModule` |
| `NewNamedBase(name string) NamedBase` | Constructor for `NamedBase` |
| `SyncCtx` | Embed to get `RuntimeCtx() context.Context` in `Start` |
| `GetInjector(ctx) do.Injector` | Retrieve the DI injector from context |
| `Provide[T](ctx, fn)` | Register a DI provider |

## pkg/config

| Symbol | Description |
|--------|-------------|
| `NewModule(opts ...Option) *Module` | Config module |
| `WithConfigDirs(dirs ...string) Option` | Set search directories for config files |
| `WithConfigName(name string) Option` | Set config file base name (default: `"lakta"`) |
| `WithArgs(args []string) Option` | Enable CLI flag overrides (`os.Args[1:]`) |
| `WithEnvPrefix(prefix string) Option` | Set env var prefix (default: `"LAKTA_"`) |
| `Bind[T](path ...string) Module` | Bind a config sub-tree to a typed struct; adds as a module |
| `Get[T](ctx) *T` | Read the current bound value (atomic, zero-alloc) |
| `GetBinding[T](ctx) *Binding[T]` | Access the binding to register `OnChange` callbacks |
| `ModulePath(category, type, instance string) string` | Generate a `modules.<category>.<type>.<instance>` path |
| `ReloadNotifier` | Subscribe to hot-reload events |
| `ReloadNotifier.OnReload(fn)` | Register a reload callback |

## pkg/testkit

| Symbol | Description |
|--------|-------------|
| `NewHarness(t) *Harness` | Injector-backed context for unit tests |
| `Harness.WithData(map) *Harness` | Seed koanf config from a map |
| `Harness.WithKoanf(*koanf.Koanf) *Harness` | Seed koanf config from a Koanf instance |
| `Harness.Ctx() context.Context` | Access the test context |
| `WithProvider[T](h, fn)` | Register a DI provider (free generic function) |
| `NewRuntimeHarness(t, modules...) *RuntimeHarness` | Full runtime in goroutine |
| `RuntimeHarness.Shutdown()` | Gracefully stop the runtime |
| `NewMockModule() *MockModule` | Basic module mock |
| `NewMockSyncModule() *MockSyncModule` | SyncModule mock with `BlockStart chan` |
| `NewMockAsyncModule() *MockAsyncModule` | AsyncModule mock |

## pkg/logging/slox

Lakta uses `github.com/Vilsol/slox` (external package):

| Symbol | Description |
|--------|-------------|
| `slox.Info(ctx, msg, attrs...)` | Log at Info level |
| `slox.Debug(ctx, msg, attrs...)` | Log at Debug level |
| `slox.Warn(ctx, msg, attrs...)` | Log at Warn level |
| `slox.Error(ctx, msg, attrs...)` | Log at Error level |
| `slox.FromContext(ctx) *slog.Logger` | Extract logger from context |
| `slox.WithContext(ctx, logger) context.Context` | Inject logger into context |
