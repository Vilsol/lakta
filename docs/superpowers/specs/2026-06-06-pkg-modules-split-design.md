# Lakta per-pkg module split — design

- **Date:** 2026-06-06
- **Status:** Revised after subagent review (pending final spec review)
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

This restructure requires **no changes to library/integration logic or import
statements**, which is what keeps the big-bang migration low-risk. The one
exception is the doc/API tooling under `cmd/` (`doccheck`, `genmodules`,
`apicheck`), which has single-module assumptions baked in and must be made
workspace-aware — see [Docs & tooling impact](#docs--tooling-impact). So it is
"near-pure `go.mod`/tag scaffolding" plus a bounded tooling update — not zero
code change.

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
| `…/pkg/testkit` | `pkg/testkit/` | (light: core + do, koanf, oops) |

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
test-only require. Precise behavior under Go 1.17+ pruning: because each
integration is a *direct* dependency of a consumer, `testkit` remains listed as
an (indirect) require in the consumer's module graph, but it is **not downloaded
or built** for the consumer's own build (the consumer never runs the
integration's tests). Its deps are light (core + do, koanf, oops), so this is
harmless — but the graph is not "free" of it. The graph is acyclic: core imports
no integration; integrations import core (and testkit for tests); testkit imports
core.

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
resolves for external consumers once all tags are pushed. `cmd` and `examples`
are not tagged.

**Tidy/tag ordering nuance (from review):** the `go mod tidy` in step 1 runs
*before* any tag exists, so it resolves `require core@<version>` against the
local workspace via `go.work`, **not** against the real tag. That means the
released `go.mod` files carry a hand-set version (step 1) that is only proven
resolvable *after* push. The release task therefore adds a **post-push
verification**: in a clean checkout with `GOWORK=off`, run
`GOFLAGS=-mod=mod go mod download` (or `go list -m github.com/Vilsol/lakta@<version>`)
for each module to confirm the pinned core version actually resolves from VCS.

**Tag-push atomicity (from review):** pushing 11 tags is not atomic — a partial
push leaves an inconsistent release. The release task pushes all tags in a single
`git push origin <tag1> <tag2> …` invocation and, on failure, deletes any
partially-created remote tags before retrying (idempotent re-run from a clean
state). This is documented as an explicit rollback step in the task.

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

**Fan-out cost (acknowledged):** 11 modules × {build, test, -race, lint,
govulncheck} is ~5× the per-check count of today's single-module CI, plus 11 tags
per release and a multiplied Renovate PR stream. This is real, recurring overhead
for a v0.0.x project and is accepted as the price of opt-in dependencies. A
GitHub Actions matrix keyed on the `go.work` module list parallelizes the CI
fan-out so wall-clock stays bounded.

`renovate.json` gains `postUpdateOptions: ["gomodTidy"]`; the `gomod` manager
auto-discovers the multiple `go.mod` files.

## Docs & tooling impact

Import *strings* are unchanged, so README and generated docs (`docs.yaml`) need
no path rewrites and `cmd/docgen` keeps working (it resolves integrations via
`go.work` locally and its own `require`s in `cmd/go.mod`). **But three `cmd/`
tools have single-module assumptions that break post-split and require code
changes** (this was the spec's biggest miss — surfaced in review):

- **`doccheck` — HIGH, will break.** `cmd/doccheck/main.go:setupTempModule`
  synthesizes a temp module that `require`s + `replace`s **only**
  `github.com/Vilsol/lakta` (core) and copies the **root** `go.sum`. After the
  split, any doc snippet importing an integration (`pkg/logging/tint`, `slog`,
  `otel`, `http/fiber`, …) targets a *separate module* the single replace does
  not cover, and the root `go.sum` no longer carries that module's deps — so the
  snippet fails to compile. **Fix:** make the temp module workspace-aware —
  either emit a `require` + `replace` per integration module (pointing at the
  in-repo dir), or run snippets inside the repo's `go.work` instead of a
  hand-built temp module. This is in-scope tooling work for the migration.
- **`genmodules` — MED.** `findRepoRoot` walks up to the *nearest* `go.mod`;
  run from a subdir post-split it would treat a submodule as the repo root.
  **Fix:** pin its working directory to the repo root (or detect the workspace
  root via `go env GOWORK`).
- **`apicheck` — MED, verify.** Its hardcoded `trackedPackages`
  (`pkg/lakta`, `pkg/config`, `pkg/testkit`) now span core **plus** the separate
  `testkit` module. **Fix/verify:** confirm its package loader is workspace-aware
  (e.g. `go/packages` honoring `go.work`); adjust if it builds from the root
  module only.

README gains a short note that integrations are installed as separate modules.

## Migration sequence (big-bang, one branch)

0. **Pre-migration audit (do first, before scaffolding):** read `cmd/doccheck`,
   `cmd/genmodules`, `cmd/apicheck` and confirm the single-module assumptions
   documented above, so their remediation is designed up front rather than
   discovered at step 8's "get green". (This is the step whose omission the
   review flagged.)
1. Remove `pkg/events/` if present (already absent in the working branch —
   confirm and no-op otherwise).
2. `go.work init` at repo root; `go work use` each module dir as it is created.
3. `go mod init <module-path>` in each integration dir + `cmd/`; add the single
   `require github.com/Vilsol/lakta` to each integration (resolved via `go.work`).
4. Move `cmd/*` ownership into the `cmd` module; add its `require`s for the
   integrations it imports.
5. Rewire `examples/microservices/go.mod` to require the integration modules
   (it imports config, lakta, db/sql, db/drivers/pgx, grpc/client, grpc/server,
   health, http/fiber, logging/slog, logging/tint, otel, workflows/temporal);
   add to `go.work`.
6. `go work sync`; `go mod tidy` every module.
7. **Update the doc/API tooling to be workspace-aware:** fix `doccheck`'s temp
   module (per-integration require+replace or run in `go.work`), pin
   `genmodules` to repo root, verify/adjust `apicheck` cross-module loading.
8. Get everything green: build, test, `-race`, lint, govulncheck, doc checks —
   per module.
9. Convert the build/test/release flows into `mise` tasks (`ci-all`, `release`,
   `verify-isolation`).
10. Update CI workflow to iterate modules; update `renovate.json`; update README
    and `CHANGELOG`.
11. One PR; on merge, run `mise run release v0.0.7`, then the post-push
    resolution verification.

## Success criteria (the acid test)

Beyond "all green per module", the split's *goal* is verified concretely:

- In `pkg/http/fiber/`: `go list -m all` contains **no** `temporal`, `pgx`, or
  `grpc` module.
- In `pkg/logging/tint/`: `go list -m all` contains **no** `otel` or `fiber`
  module.
- In `pkg/db/drivers/pgx/`: `go list -m all` contains **no** `fiber`, `grpc`, or
  `temporal` module.
- Core (`.`): asserted against an **allowlist / golden file**, not a denylist.
  Core's `go list -m all` must match the known light set (conc, do, koanf*, oops,
  slox, fsnotify, pflag, + their transitive deps); a *new* heavy dependency
  leaking into core fails the check. (Per review: a denylist of today's heavy
  deps would not catch a future leak; the golden file does.)

These assertions are the measurable proof the dependency isolation works and are
added as a `mise run verify-isolation` task so regressions are caught later. The
core golden file is regenerated deliberately (reviewed diff) when core's deps
legitimately change.

## Risks & edge cases

- **`go.work` completeness** — a module missing from `go.work` breaks local
  resolution. The release/CI tasks enumerate modules from `go work edit -json`
  so the workspace is the single source of truth.
- **Hidden cross-imports** — implementation must confirm no integration imports
  another integration's package in non-test code (the star topology says none
  do; verify with `go list` during step 7).
- **Tooling re-leak** — `cmd` must never be required by core or any published
  module (verified: only `cmd/docgen` imports integrations, and nothing imports
  `cmd`). Note (from review): "untagged" prevents version *selection* but does
  not make the module unreachable — an external `go get github.com/Vilsol/lakta/cmd`
  could still pseudo-version-fetch it. This is harmless (it is build-only tooling,
  imported by no published module), so `cmd/` is kept; a stricter alternative
  (`internal/`-rooted tooling) is noted but not adopted to avoid added complexity.
- **`internal/` boundary rule** — there are **no** `internal/` packages today
  (verified), so nothing breaks now. The rule to honor going forward: an
  `internal/` package is importable only within its own module's subtree, so any
  *future* shared helper that multiple modules need must live in **core**, not in
  an integration's `internal/`. Document this so it is not discovered the hard
  way.
- **testcontainers** — moves from the root `go.mod` into `pkg/db/drivers/pgx`
  as a test dependency (verified: sole import is `pkg/db/drivers/pgx/module_test.go`);
  it must not appear in any consumer's non-test graph.
- **golangci-lint + workspaces** — lint runs per-module dir, not once at root.
- **CHANGELOG** — with 11 synchronized modules, a single repo `CHANGELOG.md`
  keyed by version (noting which modules changed under each `v0.0.x`) is
  maintained; consumers cannot otherwise tell what changed in, e.g.,
  `pkg/grpc@v0.0.7`.
- **`go.work.sum` churn / contributor flow** — contributors must
  `go work sync` after pulling dependency changes; this is added to the README /
  contributing notes to avoid confusing local resolution failures.

## Out of scope (future work)

- **Tier-2 thin-driver pattern** — the GoFr-style core-defined interface +
  standalone-impl module + `AddX()` injection, for future *thin, driver-shaped*
  seams (cache, KV, feature flags from `IDEAS.md`). Documented as a future
  convention; not built here. Lakta's existing integrations are full
  runtime-bound `lakta.Module`s and stay import-coupled to core.
- **Dropping the `pkg/` prefix** — deferred; would be a separate, import-path-
  churning change.
