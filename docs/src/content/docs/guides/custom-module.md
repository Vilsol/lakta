---
title: Writing a Custom Module
description: Step-by-step guide to building a production-ready Lakta module.
---

This guide builds a complete custom module from scratch, following the conventions used by the built-in modules.

## File structure

Modules live in two files:

```
pkg/mymodule/
├── config.go   # Config struct, options, domain methods
└── module.go   # Module struct, lifecycle methods
```

## config.go

```go
package mymodule

import "github.com/knadh/koanf/v2"

type Config struct {
    Host    string `koanf:"host"`
    Port    int    `koanf:"port"`
    Timeout string `koanf:"timeout"`
}

type Option func(*Config)

func WithName(name string) Option {
    return func(c *Config) { c.name = name }
}

func WithHost(host string) Option {
    return func(c *Config) { c.Host = host }
}

// Domain method — produces a library-specific type
func (c *Config) Address() string {
    return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
```

## module.go

```go
package mymodule

type Module struct {
    lakta.NamedBase
    config *Config
}

func NewModule(opts ...Option) *Module {
    cfg := &Config{Host: "localhost", Port: 8080}
    for _, o := range opts { o(cfg) }
    m := &Module{config: cfg}
    m.NamedBase = lakta.NewNamedBase(cfg.name)
    return m
}

func (m *Module) ConfigPath() string {
    return "modules.mymodule." + m.Name()
}

func (m *Module) LoadConfig(k *koanf.Koanf) error {
    return k.UnmarshalWithConf("", m.config, koanf.UnmarshalConf{Tag: "koanf"})
}

func (m *Module) Init(ctx context.Context) error {
    svc := NewMyService(m.config.Address())
    lakta.Provide(ctx, func(do.Injector) (*MyService, error) {
        return svc, nil
    })
    return nil
}

func (m *Module) Shutdown(ctx context.Context) error {
    return nil
}
```

## Making it a SyncModule

If your module runs a long-lived server, implement `Start`:

```go
func (m *Module) Start(ctx context.Context) error {
    return m.server.ListenAndServe(m.config.Address()) // blocks
}
```

## Testing your module

See [Testing with testkit](/guides/testing) for how to write unit tests without a full runtime.
