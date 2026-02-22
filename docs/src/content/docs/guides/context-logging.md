---
title: Context-aware Logging
description: Use slox to log with the context-carried logger, picking up middleware-injected fields.
---

Lakta uses [`github.com/Vilsol/slox`](https://github.com/Vilsol/slox) for context-aware logging. Instead of passing a `*slog.Logger` around explicitly, `slox` retrieves the logger from the context.

## Why context-aware logging

Middleware (gRPC interceptors, HTTP middleware) can inject structured fields — trace IDs, request IDs, user IDs — into the logger on the context. Any code that uses `slox` downstream automatically inherits those fields.

## Basic usage

```go
import (
    "log/slog"
    "github.com/Vilsol/slox"
)

func (m *MyModule) handleRequest(ctx context.Context, req *Request) {
    slox.Info(ctx, "handling request",
        slog.String("user_id", req.UserID),
        slog.Int("item_count", len(req.Items)),
    )

    if err := m.process(ctx, req); err != nil {
        slox.Error(ctx, "processing failed", slog.Any("err", err))
        return
    }

    slox.Info(ctx, "request complete")
}
```

## Log levels

```go
slox.Debug(ctx, "cache miss", slog.String("key", key))
slox.Info(ctx, "server started", slog.String("addr", addr))
slox.Warn(ctx, "retry attempt", slog.Int("attempt", n))
slox.Error(ctx, "fatal query", slog.Any("err", err))
```

## Injecting fields into the context logger

Middleware can enrich the logger for all downstream calls:

```go
func withRequestID(ctx context.Context, id string) context.Context {
    logger := slox.FromContext(ctx).With(slog.String("request_id", id))
    return slox.WithContext(ctx, logger)
}
```

## Fallback behavior

If no logger has been injected into the context, `slox` falls back to `slog.Default()`, so code using `slox` is always safe to call even in tests without a full runtime.
