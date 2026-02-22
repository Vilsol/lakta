---
title: Environment Variables
description: All config keys can be overridden via environment variables.
---

Every config key supported by Lakta can be set via an environment variable. This is useful for secrets, deployment-time overrides, and container environments.

## Naming convention

Transform the dot-notation config key:

1. Prefix with `LAKTA_`
2. Convert to uppercase
3. Replace `.` with `_`

**Examples:**

| Config key | Environment variable |
|------------|---------------------|
| `modules.grpc.server.default.port` | `LAKTA_MODULES_GRPC_SERVER_DEFAULT_PORT` |
| `modules.db.pgx.primary.dsn` | `LAKTA_MODULES_DB_PGX_PRIMARY_DSN` |
| `modules.otel.default.enabled` | `LAKTA_MODULES_OTEL_DEFAULT_ENABLED` |
| `modules.http.fiber.default.port` | `LAKTA_MODULES_HTTP_FIBER_DEFAULT_PORT` |

## Priority

Environment variables override config file values but are overridden by CLI flags:

```
config file < environment variable < CLI flag
```

## Docker / Kubernetes example

```yaml
# kubernetes deployment
env:
  - name: LAKTA_MODULES_DB_PGX_DEFAULT_DSN
    valueFrom:
      secretKeyRef:
        name: db-secret
        key: dsn
  - name: LAKTA_MODULES_OTEL_DEFAULT_ENDPOINT
    value: "http://otel-collector:4317"
```
