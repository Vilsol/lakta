---
title: API Index
description: Quick reference for key types, functions, and interfaces exported by Lakta.
---

<!--
apicheck:ignore — exported symbols intentionally omitted from this curated index.
cmd/apicheck (run in CI) fails when a tracked package exports a top-level symbol
that is neither documented below nor listed here. Add a new public symbol to a
table above, or to this list with a reason.

pkg/lakta: Provider, Dependent (advanced module-authoring interfaces for init ordering)
pkg/config: Apply, Config, NewConfig, NewDefaultConfig (low-level/boilerplate config-module internals)
-->


## pkg/lakta

| Symbol | Description |
|--------|-------------|
| `NewRuntime(modules ...Module) *Runtime` | Create and run the service |
| `Runtime.Run()` | Start the runtime, block until shutdown |
| `Runtime.Validate() error` | Pre-flight dependency-graph check (cycles, unmet declared deps); no side effects |
| `ErrUnmetDependency` | Sentinel for unmet declared required deps; match via `errors.Is` |
| `RuntimeInfo` | Live module-metadata registry the runtime provides in DI; read via `Snapshot()` |
| `RuntimeInfo.Snapshot() []ModuleInfo` | Deep-copied point-in-time view of module metadata/state |
| `ModuleInfo` | Per-module metadata: name, type, init order, provides/requires/optional, lifecycle, state, init duration |
| `LifecycleKind` | Module lifecycle class: `init`/`sync`/`async` |
| `ModuleState` | Furthest lifecycle stage: `pending`/`initialized`/`started`/`stopped`/`failed` |
| `Module` | Interface: `Init(ctx) error`, `Shutdown(ctx) error` |
| `SyncModule` | Adds `Start(ctx) error` |
| `AsyncModule` | Adds `StartAsync(ctx) error` |
| `Configurable` | Adds `ConfigPath() string`, `LoadConfig(*koanf.Koanf) error` |
| `NamedModule` | Adds `Name() string` |
| `NamedBase` | Embed to satisfy `NamedModule` |
| `NewNamedBase(name string) NamedBase` | Constructor for `NamedBase` |
| `SyncCtx` | Embed to get `RuntimeCtx() context.Context` in `Start` |
| `GetInjector(ctx) do.Injector` | Retrieve the DI injector from context |
| `WithInjector(ctx, injector) context.Context` | Attach a DI injector to a context |
| `Provide[T](ctx, fn)` | Register a DI provider |
| `ProvideValue[T](ctx, value)` | Register an already-constructed value in DI |
| `Invoke[T](ctx) (T, error)` | Resolve a dependency from the context injector |
| `HasInjector(ctx) bool` | Report whether a context carries a DI injector (guards optional DI access in bare test contexts) |
| `RenderWiringReport(info []ModuleInfo, prov map[string]string) string` | Render a `RuntimeInfo` snapshot as an aligned wiring table (boot debug log / `LAKTA_DEBUG_WIRING=1` dump) |
| `HotReloadable` | Adds `OnReload(*koanf.Koanf)`; wired by the runtime for config reloads |
| `ValidatableModule` | Adds `ValidateReload(*koanf.Koanf) error`; can veto a config hot-reload before it is committed |

## pkg/config

| Symbol | Description |
|--------|-------------|
| `NewModule(opts ...Option) *Module` | Config module |
| `WithConfigDirs(dirs ...string) Option` | Set search directories for config files |
| `WithConfigName(name string) Option` | Set config file base name (default: `"lakta"`) |
| `WithArgs(args []string) Option` | Enable CLI flag overrides (`os.Args[1:]`) |
| `WithEnvPrefix(prefix string) Option` | Set env var prefix (default: `"LAKTA_"`) |
| `WithProfile(name string) Option` | Overlay `lakta.<name>.<ext>` on the base config (default: `LAKTA_PROFILE`) |
| `WithDebounceDelay(d time.Duration) Option` | Debounce window for fsnotify hot-reload events |
| `Bind[T](path ...string) Module` | Bind a config sub-tree to a typed struct; adds as a module |
| `Get[T](ctx) *T` | Read the current bound value (atomic, zero-alloc) |
| `GetBinding[T](ctx) *Binding[T]` | Access the binding to register `OnChange` callbacks |
| `ModulePath(category, type, instance string) string` | Generate a `modules.<category>.<type>.<instance>` path |
| `UnmarshalKoanf[C](c *C, k *koanf.Koanf, path string) error` | Decode a config sub-tree into a typed struct (used in `LoadConfig`) |
| `Passthrough[T]` | `map[string]any` field type that captures extra keys for raw passthrough |
| `TLS` | Embeddable file-path TLS config (`cert_file`/`key_file`/`ca_file`/`client_ca_file`/`client_auth`); builds server/client `*tls.Config` |
| `Validatable` | Adds `Validate() error`; bound configs are validated on load/reload |
| `ReloadNotifier` | Subscribe to hot-reload events |
| `ReloadNotifier.OnReload(fn)` | Register a reload callback |
| `ReloadNotifier.OnValidate(fn)` | Register a validator that can veto a reload before commit |
| `ProvenanceEntry` | Per-key config origin (`file`/`env`/`flag`/`default`) from `Module.ProvenanceSnapshot()` |

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
| `NewMockProviderModule() *MockProviderModule` | Module mock that registers a DI provider |
| `MapProvider` | `map[string]any` implementing `koanf.Provider` for seeding config |
| `WaitForAddr(t, m) net.Addr` | Block until a module reports its listen address |
| `NewSlice(t, modules...) *Slice` | Boot a subset of modules with mocked collaborators |
| `Slice.With(modules...) *Slice` | Append more modules under test |
| `Slice.WithConfig(map) *Slice` | Preset koanf config + `ReloadNotifier` |
| `Slice.WithTestLogger() *Slice` | Bridge slog records to `t.Log` |
| `Mock[T](s, v) *Slice` | Seed a double for an unmet dependency (free generic function) |
| `SliceProvide[T](s, fn) *Slice` | Register an arbitrary provider in the slice (free generic function) |
| `Slice.Validate() error` | Pre-boot check: unmet declared deps + mock/module collisions |
| `Slice.Start() *Slice` | Validate then boot the runtime with the pre-seeded injector |
| `Slice.Shutdown() error` | Gracefully stop the sliced runtime |
| `Get[T](s) T` | Invoke `T` from the slice injector or `t.Fatal` (free generic function) |
| `Slice.Provided() []string` | Names of services registered in the slice injector |
| `Slice.Notifier() *ReloadNotifier` | Shared reload notifier, for `FireReload` in tests |

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
