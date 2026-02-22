---
title: Your First Service
description: Build a minimal working Lakta service from scratch.
---

This guide walks you through building a minimal HTTP service with structured logging.

## main.go

```go
package main

import (
    "os"

    "github.com/Vilsol/lakta/pkg/config"
    fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
    "github.com/Vilsol/lakta/pkg/lakta"
    "github.com/Vilsol/lakta/pkg/logging/slog"
    "github.com/Vilsol/lakta/pkg/logging/tint"
    "github.com/gofiber/fiber/v3"
)

func main() {
    runtime := lakta.NewRuntime(
        config.NewModule(
            config.WithConfigDirs(".", "./config"),
            config.WithArgs(os.Args[1:]),
        ),
        tint.NewModule(),
        slog.NewModule(),
        fiberserver.NewModule(
            fiberserver.WithRouter(registerRoutes),
        ),
    )

    if err := runtime.Run(); err != nil {
        os.Exit(1)
    }
}
```

## Registering routes

Routes are registered via `WithRouter`, which receives the fully initialized `*fiber.App`.
DI is available inside handlers through `lakta.Invoke[T](c.Context())`.

```go
func registerRoutes(app *fiber.App) {
    app.Get("/hello", handleHello)
}

func handleHello(c fiber.Ctx) error {
    svc, err := lakta.Invoke[*MyService](c.Context())
    if err != nil {
        return err
    }
    return c.JSON(fiber.Map{"message": svc.Greet()})
}
```

## Providing a service

```go
type MyService struct{}

func (s *MyService) Greet() string { return "hello" }
```

Register it from a module's `Init`:

```go
type MyModule struct{}

func NewMyModule() *MyModule { return &MyModule{} }

func (m *MyModule) Init(ctx context.Context) error {
    lakta.Provide(ctx, func(do.Injector) (*MyService, error) {
        return &MyService{}, nil
    })
    return nil
}

func (m *MyModule) Shutdown(ctx context.Context) error { return nil }
```

Add it to the runtime before the fiber module so DI is populated when routes fire:

```go
lakta.NewRuntime(
    config.NewModule(...),
    tint.NewModule(),
    slog.NewModule(),
    NewMyModule(),        // registers *MyService
    fiberserver.NewModule(
        fiberserver.WithRouter(registerRoutes),
    ),
)
```

## config.yaml

```yaml
modules:
  http:
    fiber:
      default:
        host: "0.0.0.0"
        port: 8080
```

## Running

```bash
go run ./main.go
```
