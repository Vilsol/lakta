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
