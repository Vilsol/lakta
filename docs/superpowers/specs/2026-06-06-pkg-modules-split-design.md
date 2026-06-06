# Lakta per-pkg module split — design

- **Date:** 2026-06-06
- **Status:** Approved (pending spec review)
- **Branch:** `worktree-split-modules`

## Context & goal

Lakta is currently a single Go module (`github.com/Vilsol/lakta`). Any consumer
that imports *any* lakta package inherits the module's entire `require` list into
their module graph, `go.sum`, MVS resolution, vulnerability-scan surface, and
build. A service that only wants `tint` logging still pulls Temporal, Fiber,
pgx, gRPC, and the full OpenTelemetry stack.

**Goal:** split lakta into per-package modules so a consumer pulls only the
dependencies of the integrations it actually imports. A minimal HTTP service
should resolve `core + http/fiber` and nothing else — no gRPC, Temporal, pgx,
or DB-driver clients.

This is the problem GoFr only half-solved: GoFr decoupled its long-tail
datasources (Mongo, Cassandra, …) via interface injection but left its core
`container` importing SQL, Redis, and all pub/sub backends directly, so every
GoFr binary ships `cloud.google.com/go/pubsub`, Kafka, and MQTT clients whether
used or not. Lakta's split avoids that by making **every** integration — common
ones included — its own module, with the core holding only the runtime + config
+ DI.

## Key principle — zero import-path churn

`pkg/` is kept, and the module-path prefix is unchanged. Therefore every existing
import statement is **byte-for-byte identical** before and after:
`github.com/Vilsol/lakta/pkg/http/fiber` is the import path both as a package in
today's monolith and as its own module tomorrow. No code edits to imports in
lakta, the example, or any downstream consumer. The only consumer-visible change:
integrations are fetched separately, e.g.
`go get github.com/Vilsol/lakta/pkg/http/fiber@v0.0.7`.

This restructure is therefore **pure `go.mod`/tag scaffolding** — no logic
changes — which is what keeps the big-bang migration low-risk.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Granularity | Per-integration modules |
| DB granularity | One module per driver under `pkg/db/drivers/`; `pkg/db/sql` separate |
| `pkg/` prefix | **Kept** (deferred removal) |
| Versioning | Synchronized — all modules share one version per release |
| Version start | Continue from `v0.0.7` (no reset) |
| Migration shape | Big-bang — one branch/PR, everything green, single review |
| `pkg/events/` | Removed (empty; pub/sub is future work) |
| Local dev | Committed `go.work` |

## Module inventory

### Published & tagged (11)

| Module path | `go.mod` location | Heavy deps it owns |
|---|---|---|
| `github.com/Vilsol/lakta` (core: `pkg/lakta` + `pkg/config`) | repo root | conc, do, koanf, oops, slox, fsnotify, pflag |
| `…/pkg/logging/tint` | `pkg/logging/tint/` | tint |
| `…/pkg/logging/slog` | `pkg/logging/slog/` | otel, slog-otel |
| `…/pkg/otel` | `pkg/otel/` | otel sdk + exporters |
| `…/pkg/health` | `pkg/health/` | health-go |
| `…/pkg/http/fiber` | `pkg/http/fiber/` | fiber, mapstructure |
| `…/pkg/grpc` (client + server) | `pkg/grpc/` | grpc, grpc-middleware |
| `…/pkg/db/sql` | `pkg/db/sql/` | squirrel |
| `…/pkg/db/drivers/pgx` | `pkg/db/drivers/pgx/` | pgx, otelpgx, health-go, testcontainers |
| `…/pkg/workflows/temporal` | `pkg/workflows/temporal/` | temporal sdk |
| `…/pkg/testkit` | `pkg/testkit/` | (light: do, koanf) |

`grpc` stays a single module spanning `client` + `server` (they share the gRPC
dependency and ship together). `db` is **not** a single module: `db/sql` (the
driver-agnostic query builder) and each `db/drivers/<driver>` are independent
modules. The `db/drivers/` namespace grows one module per future driver
(redis, cassandra, …), each opt-in — deliberately avoiding the GoFr Tier-1
bloat where a growing datasource category gets bundled into one import.

### Dev-only (in `go.work`, never tagged, never consumed as a dependency)

- `…/cmd` (`go.mod` at `cmd/`) — `docgen`, `doccheck`, `apicheck`, `genmodules`.
  `cmd/docgen` imports nine integrations to generate docs. Isolating it in its
  own module is **load-bearing**: if this tooling lived in core, core's `go.mod`
  would `require` every integration, and since core is what all consumers import,
  that would re-leak every dependency — defeating the split.
- `…/examples/microservices` — already its own module; rewired to require the
  integration modules and joined to `go.work`.

## Dependency graph — pure 2-level star

Every integration module's `go.mod` has exactly **one** intra-repo require: core
(`github.com/Vilsol/lakta`). No integration requires another integration — there
is no leaf-to-leaf coupling (verified: `grpc` client+server and the db modules
each import only `pkg/config` + `pkg/lakta` plus their own libraries; `db/sql`
and `db/drivers/pgx` do not import each other).

`testkit` is imported by integrations **only in `_test.go`**, so it is a
test-only require — Go's module-graph pruning keeps it out of downstream consumer
graphs. The graph is acyclic: core imports no integration; integrations import
core (and testkit for tests); testkit imports core.

## Local development — committed `go.work`

A `go.work` at the repo root `use`s all 13 modules — the 11 published modules
plus the `cmd` and `examples` dev-only modules. Committed, alongside
`go.work.sum`. `go.work` only affects builds *within* the repo; it is ignored
when a module is consumed as an external dependency, so committing it is safe and
is the standard monorepo practice.
Cross-module edits and `cmd/docgen` resolve against local source — no published
tags needed during development or CI.

## Synchronized release — all tags on one commit

A `mise run release <version>` task (script under `mise-tasks/` or inline):

1. For each integration `go.mod`, set `require github.com/Vilsol/lakta` →
   `<version>`, then `go mod tidy`.
2. Commit the `go.mod` changes.
3. Create **all 11 tags on that single commit**:
   `v0.0.7` (core), `pkg/logging/tint/v0.0.7`, `pkg/logging/slog/v0.0.7`,
   `pkg/otel/v0.0.7`, `pkg/health/v0.0.7`, `pkg/http/fiber/v0.0.7`,
   `pkg/grpc/v0.0.7`, `pkg/db/sql/v0.0.7`, `pkg/db/drivers/pgx/v0.0.7`,
   `pkg/workflows/temporal/v0.0.7`, `pkg/testkit/v0.0.7`.
4. Push all tags together.

Go requires each tag's prefix to equal the module's subdirectory. Because every
tag points at the **same commit**, an integration's `require core@v0.0.7`
resolves immediately — there is no chicken-and-egg ordering window. `cmd` and
`examples` are not tagged.

**Future caution (documented, not actioned):** synchronized/lockstep versioning
holds only while all modules share a stability level. If one module later
stabilizes at v1 while another stays v0 (the situation that forced OpenTelemetry
to build its `multimod` tool, and that Kratos sidesteps by versioning core and
contrib independently), peel that module onto its own cadence then.

## CI

The single-module `go build ./...` / `mise run test` / `mise run lint` steps only
see the core module after the split. Replace them with iteration over the
workspace modules:

- New `mise run ci-all` (or a GitHub Actions matrix) loops the module dirs from
  `go work edit -json` and runs, per module: `go build ./...`, `go test`,
  `go test -race`, `golangci-lint run`, `govulncheck`.
- `doccheck` / `docgen-check` / `apicheck` run from the `cmd` module.
- Per-module `coverage.out` profiles are merged for the aggregate coverage number.

`renovate.json` gains `postUpdateOptions: ["gomodTidy"]`; the `gomod` manager
auto-discovers the multiple `go.mod` files. More `go.mod` files means more
Renovate PRs — accepted.

## Docs & tooling impact

- Import paths are unchanged, so README, generated docs (`docs.yaml`), and the
  docgen pipeline need no path rewrites. README gains a short note that
  integrations are installed as separate modules.
- `cmd/docgen` continues to import every integration; it resolves them via
  `go.work` locally and via its own `require`s in `cmd/go.mod`.

## Migration sequence (big-bang, one branch)

1. Remove the empty `pkg/events/` directory.
2. `go.work init` at repo root; `go work use` each module dir as it is created.
3. `go mod init <module-path>` in each integration dir + `cmd/`; add the single
   `require github.com/Vilsol/lakta` to each integration (resolved via `go.work`).
4. Move `cmd/*` ownership into the `cmd` module; add its `require`s for the
   integrations it imports.
5. Rewire `examples/microservices/go.mod` to require the integration modules;
   add to `go.work`.
6. `go work sync`; `go mod tidy` every module.
7. Get everything green: build, test, `-race`, lint, govulncheck, doc checks —
   per module.
8. Convert the build/test/release flows into `mise` tasks (`ci-all`, `release`).
9. Update CI workflow to iterate modules; update `renovate.json`; update README.
10. One PR; on merge, run `mise run release v0.0.7`.

## Success criteria (the acid test)

Beyond "all green per module", the split's *goal* is verified concretely:

- In `pkg/http/fiber/`: `go list -m all` contains **no** `temporal`, `pgx`, or
  `grpc` module.
- In `pkg/logging/tint/`: `go list -m all` contains **no** `otel` or `fiber`
  module.
- In `pkg/db/drivers/pgx/`: `go list -m all` contains **no** `fiber`, `grpc`, or
  `temporal` module.
- Core (`.`): `go list -m all` contains **no** fiber/grpc/temporal/pgx/otel.

These assertions are the measurable proof the dependency isolation works and are
added as a `mise run verify-isolation` task so regressions are caught later.

## Risks & edge cases

- **`go.work` completeness** — a module missing from `go.work` breaks local
  resolution. The release/CI tasks enumerate modules from `go work edit -json`
  so the workspace is the single source of truth.
- **Hidden cross-imports** — implementation must confirm no integration imports
  another integration's package in non-test code (the star topology says none
  do; verify with `go list` during step 7).
- **Tooling re-leak** — `cmd` must never be required by core or any published
  module. Enforced by it being a separate, untagged module.
- **testcontainers** — moves from the root `go.mod` into `pkg/db/drivers/pgx`
  as a test dependency; it must not appear in any consumer's non-test graph.
- **golangci-lint + workspaces** — lint runs per-module dir, not once at root.

## Out of scope (future work)

- **Tier-2 thin-driver pattern** — the GoFr-style core-defined interface +
  standalone-impl module + `AddX()` injection, for future *thin, driver-shaped*
  seams (cache, KV, feature flags from `IDEAS.md`). Documented as a future
  convention; not built here. Lakta's existing integrations are full
  runtime-bound `lakta.Module`s and stay import-coupled to core.
- **Dropping the `pkg/` prefix** — deferred; would be a separate, import-path-
  churning change.
