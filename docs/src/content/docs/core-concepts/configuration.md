---
title: Configuration
description: The koanf-based config system — files, env vars, CLI flags, hot-reload, and struct binding.
---

Lakta uses [`github.com/knadh/koanf/v2`](https://github.com/knadh/koanf) for configuration. The config module loads values from multiple sources, merges them in priority order, and provides the result to the rest of the application via DI.

## Setup

```go compile imports="os,github.com/Vilsol/lakta/pkg/config"
config.NewModule(
    config.WithConfigDirs(".", "./config"),
    config.WithArgs(os.Args[1:]),
)
```

The config module declares `Provides()` for `*koanf.Koanf` and `config.ReloadNotifier`, so the runtime automatically initializes it before any module that depends on those types. Placing it first in `NewRuntime` is conventional but not required.

It registers two types in DI:
- `*koanf.Koanf` — the merged configuration tree
- `config.ReloadNotifier` — used to subscribe to hot-reload events

## Sources (lowest → highest priority)

```
config files  <  environment variables  <  CLI flags
```

### Config files

By default the module searches for a file named `lakta` with any supported extension (`.yaml`, `.yml`, `.json`, `.toml`) in the directories passed to `WithConfigDirs`. Files from all directories are loaded and merged.

```go compile imports="github.com/Vilsol/lakta/pkg/config"
config.NewModule(
    config.WithConfigDirs(".", "./config", "/etc/myapp"),
    config.WithConfigName("myapp"), // default: "lakta"
)
```

Multiple files are merged left-to-right — later files override earlier ones. Use this for a base config plus environment-specific overrides:

```yaml
# config/lakta.yaml — base
modules:
  grpc:
    server:
      default:
        port: 50051

# lakta.yaml — local override
modules:
  grpc:
    server:
      default:
        port: 9090
```

### Environment variables

Variables prefixed with `LAKTA_` map to dot-notation config keys. The prefix is stripped, underscores become dots, everything is lowercased:

```
LAKTA_MODULES_GRPC_SERVER_DEFAULT_PORT=9090
  → modules.grpc.server.default.port
```

Override the prefix:

```go compile imports="github.com/Vilsol/lakta/pkg/config"
config.WithEnvPrefix("MYAPP_")
```

### CLI flags

Pass `os.Args[1:]` to enable `--key=value` overrides:

```go compile imports="os,github.com/Vilsol/lakta/pkg/config"
config.WithArgs(os.Args[1:])
```

CLI flags are matched against keys already present in koanf (from files and env vars):

```bash
./myapp --modules.grpc.server.default.port=9090
./myapp --app.debug=true
```

## Key naming convention

All module config lives under `modules.<category>.<type>.<instance>`:

```yaml
modules:
  grpc:
    server:
      default:
        host: "0.0.0.0"
        port: 50051
      internal:
        host: "127.0.0.1"
        port: 50052
    client:
      data:
        target: "data-service:50051"
  db:
    pgx:
      main:
        dsn: "postgres://localhost/mydb"
```

Use `config.ModulePath` to generate paths consistently in code:

```go compile imports="github.com/Vilsol/lakta/pkg/config"
config.ModulePath(config.CategoryGRPC, "server", "default")   // "modules.grpc.server.default"
config.ModulePath(config.CategoryGRPC, "server", "internal")  // "modules.grpc.server.internal"
config.ModulePath(config.CategoryDB,   "pgx",    "main")      // "modules.db.pgx.main"
```

## Binding structs with config.Bind

`config.Bind[T]` is the high-level API for wiring config into typed structs with automatic hot-reload. Add it as a module in the runtime:

```go compile=stmt imports="github.com/Vilsol/lakta/pkg/lakta,github.com/Vilsol/lakta/pkg/config"
type AppConfig struct {
    Workers int  `koanf:"workers"`
    Debug   bool `koanf:"debug"`
}

lakta.NewRuntime(
    config.NewModule(config.WithConfigDirs(".")),
    config.Bind[AppConfig]("app"),
)
```

Read the bound value anywhere you have a context:

```go compile=stmt imports="context,fmt,github.com/Vilsol/lakta/pkg/config"
type AppConfig struct{ Workers int }
cfg := config.Get[AppConfig](context.Background())
fmt.Println(cfg.Workers)
```

### Nested paths

```go compile=stmt imports="github.com/Vilsol/lakta/pkg/config"
type ServerConfig struct{}
config.Bind[ServerConfig]("modules", "grpc", "server", "default")
// reads from: modules.grpc.server.default
```

### Reacting to changes

```go compile=stmt imports="context,fmt,github.com/Vilsol/lakta/pkg/config"
type AppConfig struct{ Workers int }
binding := config.GetBinding[AppConfig](context.Background())
binding.OnChange(func(cfg *AppConfig) {
    // called after the new value is stored — config.Get returns new value here too
    fmt.Printf("workers updated: %d\n", cfg.Workers)
})
```

### Validation

If the struct implements `Validate() error`, it is called after every unmarshal. A failure at startup aborts `Init`. On reload, the old value is preserved:

```go compile=decl imports="errors"
type AppConfig struct{ Workers int }

func (c *AppConfig) Validate() error {
    if c.Workers <= 0 {
        return errors.New("workers must be positive")
    }
    return nil
}
```

## Hot-reload

When config files change on disk, the module reloads them automatically (debounced, default 100ms). The reload sequence re-applies all sources in priority order, then notifies callbacks.

Subscribe directly via `ReloadNotifier`:

```go compile=stmt imports="context,github.com/knadh/koanf/v2,github.com/Vilsol/lakta/pkg/config,github.com/Vilsol/lakta/pkg/lakta,github.com/samber/do/v2"
notifier, _ := do.Invoke[config.ReloadNotifier](lakta.GetInjector(context.Background()))
notifier.OnReload(func(k *koanf.Koanf) {
    _ = k.Int("modules.grpc.server.default.port")
})
```

> Callbacks run while the config module holds its write lock. Do not call back into the config module from inside a callback.

## Writing a Configurable module

Implement the `Configurable` interface to have the runtime populate your config struct before `Init` runs:

```go compile=decl imports="context,github.com/knadh/koanf/v2,github.com/Vilsol/lakta/pkg/config"
type Config struct {
    Workers int    `koanf:"workers"`
    Queue   string `koanf:"queue"`
}

type Module struct{ config Config }

func (m *Module) ConfigPath() string {
    return config.ModulePath("workflows", "processor", "default")
}

func (m *Module) LoadConfig(k *koanf.Koanf) error {
    return config.UnmarshalKoanf(&m.config, k, "")
}

func (m *Module) Init(ctx context.Context) error {
    // m.config is fully populated here
    return nil
}
```

## Testing

Use `testkit.NewHarness` to provide config without the file system:

```go compile=test imports="github.com/Vilsol/lakta/pkg/testkit"
h := testkit.NewHarness(t).WithData(map[string]any{
    "app": map[string]any{"workers": 8, "debug": true},
})
_ = h
```

Simulate hot-reload:

```go compile=test imports="github.com/knadh/koanf/v2,github.com/Vilsol/lakta/pkg/testkit"
h := testkit.NewHarness(t)
newK := koanf.New(".")
_ = newK.Load(testkit.MapProvider(map[string]any{"app": map[string]any{"workers": 16}}), nil)
h.Notifier().FireReload(newK)
```

## Quick reference

| Goal | How |
|------|-----|
| Load from files | `config.WithConfigDirs(...)`, `config.WithConfigName(...)` |
| Override via env | `LAKTA_<KEY>` (underscores → dots) |
| Override via CLI | `config.WithArgs(os.Args[1:])`, then `--key=value` |
| Change env prefix | `config.WithEnvPrefix("MYAPP_")` |
| Bind to a struct | `config.Bind[T]("path")` as a module |
| Read bound value | `config.Get[T](ctx)` |
| React to reload | `config.GetBinding[T](ctx).OnChange(fn)` |
| Validate on load | Implement `Validate() error` on the struct |
| Generate module path | `config.ModulePath(category, type, instance)` |
| Raw koanf access | `do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))` |
| Test without files | `testkit.NewHarness(t).WithData(map[string]any{...})` |
| Simulate reload in tests | `h.Notifier().FireReload(newKoanf)` |
