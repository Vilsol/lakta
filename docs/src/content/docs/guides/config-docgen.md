---
title: Generating Config Docs
description: Generate drift-proof config documentation and a JSON Schema for your service's modules with pkg/reflectcfg.
---

`github.com/Vilsol/lakta/pkg/reflectcfg` is the reflection library behind lakta's own `docgen`. It is public, so any service built on lakta can generate accurate config documentation — field names, types, defaults, env-var names, descriptions — and a Draft 2020-12 JSON Schema for the exact set of modules it registers, lakta built-ins and its own alike.

Generated docs cannot drift: names and defaults come from the config structs, env-var names from each module's `ConfigPath()`, and descriptions from the Go doc comments on config fields.

## Requirements

Your modules already qualify if they follow the standard module contract:

- a package-level `NewDefaultConfig()`,
- `ConfigPath()` on `*Module` (the `lakta.Configurable` half),
- `koanf` struct tags (plus optional `required`/`enum` tags).

Field doc comments on the `Config` struct become the field descriptions, and `WithXxx` option doc comments document code-only options. Source must be resolvable via `go list` (module cache, `replace`, or workspace — any normal build environment).

## The generator

Add a small program to your service, e.g. `tools/docgen/main.go`:

```go
package main

import (
	"os"

	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/reflectcfg"

	"example.com/myservice/pkg/mymodule"
)

func main() {
	entries := []reflectcfg.Entry{
		reflectcfg.FromModule(grpcserver.NewModule(), grpcserver.NewDefaultConfig()),
		reflectcfg.FromModule(mymodule.NewModule(), mymodule.NewDefaultConfig()),
	}

	out := reflectcfg.Reflect(entries, nil)

	if err := reflectcfg.EncodeSchema(os.Stdout, out, "https://example.com/myservice.schema.json"); err != nil {
		os.Exit(1)
	}
}
```

`FromModule` pairs a module's declared `ConfigPath()` with its default config value, so category, type, and env-var names are taken from the same source the runtime uses to load config. `EncodeYAML` emits the doc tree instead of a schema if you want to render docs pages from it.

To include dependency versions in passthrough docs links, pass `reflectcfg.ParseGoMod()` output as the second argument to `Reflect`.

## What you get

Given a module whose `ConfigPath()` is `modules.custom.widget.<instance>` and this config struct:

```go
// Config configures the widget module.
type Config struct {
	// Host is the bind address.
	Host string `koanf:"host"`
	// Port is the listen port.
	Port int `koanf:"port" required:"true"`
	Name string `koanf:"-"`
}

func NewDefaultConfig() Config {
	return Config{Host: "0.0.0.0", Port: 8080, Name: "default"}
}
```

`Reflect` + `EncodeYAML` produce:

```yaml
modules:
  - category: custom
    type: widget
    package: example.com/myservice/pkg/widget
    configPath: modules.custom.widget.<name>
    description: configures the widget module
    fields:
      - key: host
        type: string
        default: 0.0.0.0
        envVar: LAKTA_MODULES__CUSTOM__WIDGET__<NAME>__HOST
        description: host is the bind address
      - key: port
        type: int
        default: "8080"
        required: true
        envVar: LAKTA_MODULES__CUSTOM__WIDGET__<NAME>__PORT
        description: port is the listen port
```

`EncodeSchema` emits the same information as a Draft 2020-12 JSON Schema: one `$defs` entry per module type, `required` for non-pointer `required:"true"` fields, enum values from `enum` tags, a duration pattern for `time.Duration` fields, and `additionalProperties: false` everywhere except `Passthrough` blocks.

Also captured, when present:

- **Nested structs** (same-package struct fields with a `koanf` tag) become nested `fields` trees with dot-notation env vars.
- **Slices and maps of structs** (`[]T` / `map[string]T` where `T` is a same-package struct) document the element's fields under `fields`, and the schema types them as `items` / `additionalProperties` objects. Element fields carry no env vars (collection elements aren't individually env-addressable — only the parent field keeps one), and their defaults come from the first element of the default slice (maps document zero defaults). External element types stay opaque.
- **Code-only options** (`koanf:"-"` fields tagged `code_only`) are listed under `codeOnly` with the matching `WithXxx` option's doc comment — and excluded from the schema.
- **Passthrough blocks** (`config.Passthrough[T]`) record the target type, package, and a pkg.go.dev link when versions are supplied via `ParseGoMod`.

## API surface

| Symbol | Purpose |
|---|---|
| `FromModule(mod, cfg) Entry` | Pair a module's `ConfigPath()` with its default config value |
| `Entry{Path, Config}` | Explicit form; empty or non-canonical `Path` falls back to package-path inference |
| `Reflect(entries, modVersions) Output` | Build the doc tree; `modVersions` (from `ParseGoMod`) is optional |
| `EncodeYAML(w, out)` | Emit the doc tree as YAML |
| `EncodeSchema(w, out, id)` / `BuildSchema(out, id)` | Emit / build a Draft 2020-12 JSON Schema with the given `$id` |
| `ParseGoMod()` | Collect dependency versions from `go.work`/`go.mod` for passthrough doc links |

`Config` values may be passed by value or as pointers.

## Keeping it in sync

Two conventions make the output drift-proof:

1. **Share the module list.** Factor your service's module registrations into one function used by both `main()` and the generator, so the docs always cover exactly what the runtime registers.
2. **Check drift in CI.** Wire the generator to `go generate`, commit the output, and fail CI when regeneration changes it:

```sh
go run ./tools/docgen > myservice.schema.json
git diff --exit-code -- myservice.schema.json
```

## IDE validation

Serve the generated schema over HTTPS (or commit it to your repo) and reference it in config files:

```yaml
# yaml-language-server: $schema=https://example.com/myservice.schema.json
modules:
  grpc:
    server:
      default:
        port: 50051
```

Lakta's own schema for the built-in modules is published at `https://vilsol.github.io/lakta/lakta.schema.json` — see the Config Schema reference.
