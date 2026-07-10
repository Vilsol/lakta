---
title: Production Checklist
description: What to verify before a Lakta service takes production traffic.
---

A pass over every switch worth flipping before a Lakta service takes real traffic. Each item links to the page that covers it in depth.

## Configuration

- [ ] **Secrets come from the environment**, not config files — every key has a `LAKTA_...` form ([reference](/lakta/reference/env-vars/))
- [ ] **Config files validate in CI/IDE** against the [published JSON schema](/lakta/reference/config-schema/)
- [ ] **Hot-reload behavior is intentional** — modules implementing `HotReloadable` pick up config changes live; make sure that's what you want for each ([configuration](/lakta/core-concepts/configuration/))

## Traffic

- [ ] **HTTP timeouts are set** — Fiber's read/write/idle timeouts default to off; set them via config or `WithDefaults` ([HTTP module](/lakta/modules/http/))
- [ ] **gRPC server has keepalive and message-size limits** appropriate to your clients ([gRPC server](/lakta/modules/grpc-server/))
- [ ] **Readiness gates on real dependencies** — register a health check per hard dependency (database ping, downstream API) so the pod leaves rotation when they fail ([health](/lakta/modules/health/))

## Observability

- [ ] **OpenTelemetry exports somewhere** — set the otel `endpoint` and `service_name`; traces, metrics, and logs are wired through automatically ([otel module](/lakta/modules/otel/))
- [ ] **Log level is `info` or higher** and structured logging goes through `slox` with context, so trace IDs land on every line ([logging](/lakta/modules/logging/), [context-aware logging](/lakta/guides/context-logging/))

## Lifecycle

- [ ] **`terminationGracePeriodSeconds` exceeds 30s** — the runtime needs its full shutdown deadline ([deployment](/lakta/guides/deployment/))
- [ ] **Long-running work respects context cancellation** — `Start` blocks until ctx cancels; goroutines spawned in `StartAsync` must exit on cancel ([lifecycle](/lakta/getting-started/lifecycle/))
- [ ] **Init order is declared, not assumed** — modules that need each other declare `Provides`/`Dependencies` rather than relying on argument order ([modules](/lakta/core-concepts/modules/))

## Attack surface

- [ ] **Debug/actuator endpoints are disabled, authed, or absent** — pprof and config dumps do not belong on the public internet
- [ ] **Auth middleware covers every non-public route** — verify the deny-by-default posture rather than opting individual routes in

## Last

- [ ] **Kill a pod under load** and watch it drain cleanly (no 5xx spike, no dropped in-flight requests)
- [ ] **Run the service with `-race` in CI** — the framework's own gates do; yours should too
