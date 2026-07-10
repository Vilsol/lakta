---
title: Deploying to Production
description: Containerize a Lakta service and wire it into Kubernetes with health probes, graceful shutdown, and environment-based config.
---

Lakta services are single static binaries with built-in health endpoints, SIGTERM handling, and environment-variable config overrides — everything a container platform expects.

## Docker image

A standard multi-stage build produces a minimal image:

```dockerfile title="Dockerfile"
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /service .

FROM gcr.io/distroless/static-debian12
COPY --from=build /service /service
COPY config/ /config/
ENTRYPOINT ["/service"]
```

Point the config module at the baked-in directory (and the working directory for local runs):

```go compile=skip
config.NewModule(
    config.WithConfigDirs(".", "/config"),
    config.WithArgs(os.Args[1:]),
),
```

## Kubernetes probes

With the `health` and `fiber` modules registered, the HTTP server exposes `GET /health/live` and `GET /health/ready`. Wire them to the corresponding probes:

```yaml title="deployment.yaml"
containers:
  - name: service
    image: registry.example.com/service:latest
    ports:
      - containerPort: 8080
    livenessProbe:
      httpGet: { path: /health/live, port: 8080 }
      periodSeconds: 10
    readinessProbe:
      httpGet: { path: /health/ready, port: 8080 }
      periodSeconds: 5
```

Register a check for every hard dependency so readiness reflects reality — a service that can't reach its database should not receive traffic:

```go compile=skip
h := do.MustInvoke[*health.Health](lakta.GetInjector(ctx))
h.Register(health.Config{
    Name:  "database",
    Check: func(ctx context.Context) error { return pool.Ping(ctx) },
})
```

## Graceful shutdown

On `SIGTERM` the runtime shuts modules down in reverse init order with a **30-second deadline**. Give Kubernetes a slightly longer grace period so the runtime — not the kubelet — controls the deadline:

```yaml
spec:
  terminationGracePeriodSeconds: 35
```

No signal-handling code is needed in your service; the runtime owns it.

## Configuration via environment

Every config key can be overridden with a `LAKTA_`-prefixed environment variable (double underscore separates path segments). This is the natural place for per-environment values and secrets:

```yaml
env:
  - name: LAKTA_MODULES__DB__PGX__DEFAULT__DSN
    valueFrom:
      secretKeyRef: { name: service-secrets, key: database-dsn }
  - name: LAKTA_MODULES__LOGGING__SLOG__DEFAULT__LEVEL
    value: info
```

The full key-to-variable mapping is generated in the [Environment Variables reference](/lakta/reference/env-vars/).

## OpenTelemetry export

The `otel` module reads standard exporter settings from its config block; in a cluster you typically point it at a local collector:

```yaml
env:
  - name: LAKTA_MODULES__OTEL__OTEL__DEFAULT__ENDPOINT
    value: otel-collector.observability:4317
```

## Debug endpoints

If you register the `actuator` module, its `/debug` endpoints (pprof, goroutines, config) are powerful — and sensitive. In production either disable the sensitive groups, require auth, or don't register the module at all. See the [production checklist](/lakta/guides/production-checklist/).
