---
title: Config Passthrough
description: Forward arbitrary config keys directly into a library's config struct using Raw and mapstructure.
---

Some libraries have large config structs with dozens of fields. Rather than mapping every field explicitly in Lakta's `Config` struct, you can use the **raw passthrough** pattern to forward unknown fields directly to the underlying library.

## How it works

Declare a `Raw` field with `` koanf:",remain" `` to capture every key not explicitly handled:

```go
type Config struct {
    Host string         `koanf:"host"`
    Port int            `koanf:"port"`
    Raw  map[string]any `koanf:",remain"`
}
```

Then use `mapstructure` to decode `Raw` into the library's config struct:

```go
import "github.com/mitchellh/mapstructure"

func (c *Config) ToFiberConfig() fiber.Config {
    cfg := fiber.Config{}
    _ = mapstructure.Decode(c.Raw, &cfg)
    return cfg
}
```

## Example config

```yaml
modules:
  http:
    fiber:
      default:
        host: "0.0.0.0"
        port: 8080
        # These fall into Raw and get decoded into fiber.Config:
        body_limit: 10485760
        read_timeout: "30s"
        write_timeout: "30s"
        enable_trusted_proxy_check: true
```

## When to use this pattern

Use raw passthrough when:

- The underlying library has a large or frequently-changing config struct
- You want users to control library internals without Lakta adding wrapper fields for each one
- The library's config field names match reasonable YAML keys

Avoid it when you need validation, documentation, or defaults for specific fields — in those cases, declare the field explicitly in your `Config` struct.
