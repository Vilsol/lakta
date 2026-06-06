# Per-pkg Module Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split lakta into per-package Go modules so a consumer pulls only the dependencies of the integrations it imports, keeping the `pkg/` prefix so no import strings change.

**Architecture:** Multi-module monorepo. Root `go.mod` = core (`pkg/lakta` + `pkg/config`). Each integration under `pkg/` becomes its own module requiring core. A committed `go.work` ties them together for local dev/CI; `cmd/` and `examples/microservices` are dev-only modules (untagged). Synchronized versioning, all tags on one commit. The only library-code change is making `cmd/doccheck` workspace-aware.

**Tech Stack:** Go 1.26, go workspaces (`go.work`), `golang.org/x/mod/modfile`, mise tasks, golangci-lint 2.12.2, govulncheck, GitHub Actions.

**Reference spec:** `docs/superpowers/specs/2026-06-06-pkg-modules-split-design.md`

**Module inventory (11 published + 2 dev-only):**

| Module path | go.mod dir |
|---|---|
| `github.com/Vilsol/lakta` (core) | `.` (repo root) |
| `…/pkg/logging/tint` | `pkg/logging/tint` |
| `…/pkg/logging/slog` | `pkg/logging/slog` |
| `…/pkg/otel` | `pkg/otel` |
| `…/pkg/health` | `pkg/health` |
| `…/pkg/http/fiber` | `pkg/http/fiber` |
| `…/pkg/grpc` (client+server) | `pkg/grpc` |
| `…/pkg/db/sql` | `pkg/db/sql` |
| `…/pkg/db/drivers/pgx` | `pkg/db/drivers/pgx` |
| `…/pkg/workflows/temporal` | `pkg/workflows/temporal` |
| `…/pkg/testkit` | `pkg/testkit` |
| `…/cmd` (dev-only, untagged) | `cmd` |
| `…/examples/microservices` (dev-only) | `examples/microservices` |

---

## Task 1: Establish green baseline and initialize the workspace

**Files:**
- Create: `go.work`
- Verify: repo root

- [ ] **Step 1: Confirm the pre-split state is green**

Run:
```bash
go build ./... && mise run test && mise run lint
```
Expected: all pass. This is the baseline the split must preserve.

- [ ] **Step 2: Confirm `pkg/events/` is absent (spec cleanup)**

Run:
```bash
test -d pkg/events && echo "PRESENT — remove it" || echo "absent (ok)"
```
Expected: `absent (ok)`. If PRESENT, run `git rm -r pkg/events`.

- [ ] **Step 3: Initialize the workspace with core only**

Run:
```bash
go work init
go work use .
```
Expected: creates `go.work` containing `use .` and a `go 1.26` line.

- [ ] **Step 4: Verify core still builds under the workspace**

Run:
```bash
go build ./...
```
Expected: PASS (unchanged behavior; workspace with a single module is a no-op).

- [ ] **Step 5: Commit**

```bash
git add go.work
git commit -m "chore(modules): initialize go.work with core module"
```

---

## Task 2: Initialize all sub-module go.mod files and register them in the workspace

This task only *creates* bare `go.mod` files and registers them; dependency resolution happens in Task 3. Splitting init from tidy guarantees every module is workspace-visible before any `go mod tidy` runs (so test-only cross-edges like integrations→testkit resolve locally).

**Files:**
- Create: `pkg/logging/tint/go.mod`, `pkg/logging/slog/go.mod`, `pkg/otel/go.mod`, `pkg/health/go.mod`, `pkg/http/fiber/go.mod`, `pkg/grpc/go.mod`, `pkg/db/sql/go.mod`, `pkg/db/drivers/pgx/go.mod`, `pkg/workflows/temporal/go.mod`, `pkg/testkit/go.mod`, `cmd/go.mod`
- Modify: `go.work`

- [ ] **Step 1: Init each integration module and the cmd module**

Run (exact module path per directory):
```bash
( cd pkg/logging/tint        && go mod init github.com/Vilsol/lakta/pkg/logging/tint )
( cd pkg/logging/slog        && go mod init github.com/Vilsol/lakta/pkg/logging/slog )
( cd pkg/otel                && go mod init github.com/Vilsol/lakta/pkg/otel )
( cd pkg/health              && go mod init github.com/Vilsol/lakta/pkg/health )
( cd pkg/http/fiber          && go mod init github.com/Vilsol/lakta/pkg/http/fiber )
( cd pkg/grpc                && go mod init github.com/Vilsol/lakta/pkg/grpc )
( cd pkg/db/sql              && go mod init github.com/Vilsol/lakta/pkg/db/sql )
( cd pkg/db/drivers/pgx      && go mod init github.com/Vilsol/lakta/pkg/db/drivers/pgx )
( cd pkg/workflows/temporal  && go mod init github.com/Vilsol/lakta/pkg/workflows/temporal )
( cd pkg/testkit             && go mod init github.com/Vilsol/lakta/pkg/testkit )
( cd cmd                     && go mod init github.com/Vilsol/lakta/cmd )
```
Expected: each prints `go: creating new go.mod`.

- [ ] **Step 2: Register every module in the workspace**

Run:
```bash
go work use \
  ./pkg/logging/tint ./pkg/logging/slog ./pkg/otel ./pkg/health \
  ./pkg/http/fiber ./pkg/grpc ./pkg/db/sql ./pkg/db/drivers/pgx \
  ./pkg/workflows/temporal ./pkg/testkit ./cmd ./examples/microservices
go work sync
```
Expected: `go.work` now lists all 13 modules; `go work sync` exits 0.

- [ ] **Step 3: Sanity-check the workspace module list**

Run:
```bash
go work edit -json | python3 -c 'import json,sys; print("\n".join(u["DiskPath"] for u in json.load(sys.stdin)["Use"]))'
```
Expected: 13 paths printed (`.`, the 10 integrations, `cmd`, `examples/microservices`).

- [ ] **Step 4: Commit (WIP — go.mod files are still bare)**

```bash
git add go.work pkg/*/go.mod pkg/**/go.mod cmd/go.mod
git commit -m "chore(modules): scaffold per-pkg module go.mod files and register in workspace"
```

---

## Task 3: Tidy every module and trim core

After this task, each integration's `go.mod` declares only what it imports, core sheds the now-external heavy deps, and everything builds via the workspace.

**Files:**
- Modify: every `go.mod` from Task 2 + root `go.mod` + `examples/microservices/go.mod`

- [ ] **Step 1: Tidy each integration and the testkit module**

Run:
```bash
for d in pkg/logging/tint pkg/logging/slog pkg/otel pkg/health \
         pkg/http/fiber pkg/grpc pkg/db/sql pkg/db/drivers/pgx \
         pkg/workflows/temporal pkg/testkit; do
  ( cd "$d" && go mod tidy )
done
```
Expected: each gains `require github.com/Vilsol/lakta <version>` plus its own third-party deps. (In-workspace, the core version may be written as a `v0.0.0-…` pseudo-version placeholder; this is overridden locally by `go.work` and replaced with the real tag by the release task in Task 6 — do not hand-edit it.)

- [ ] **Step 2: Tidy the cmd module**

Run:
```bash
( cd cmd && go mod tidy )
```
Expected: `cmd/go.mod` gains requires for core + the nine integrations `cmd/docgen` imports (pgx, grpc/client+server via the grpc module, health, http/fiber, logging/slog, logging/tint, otel, workflows/temporal) + `golang.org/x/mod`.

- [ ] **Step 3: Trim the core (root) module**

Run:
```bash
go mod tidy
```
Expected: root `go.mod` loses fiber, grpc, temporal, pgx, otel exporters, health-go, squirrel, etc. — keeping only core deps (conc, do, koanf*, oops, slox, fsnotify, pflag and their transitive deps).

- [ ] **Step 4: Rewire and tidy the example module**

Run:
```bash
( cd examples/microservices && go mod tidy )
```
Expected: `examples/microservices/go.mod` now requires the integration modules it imports (config+lakta via core, db/sql, db/drivers/pgx, grpc, health, http/fiber, logging/slog, logging/tint, otel, workflows/temporal).

- [ ] **Step 5: Build every module**

Run:
```bash
for d in $(go work edit -json | python3 -c 'import json,sys;[print(u["DiskPath"]) for u in json.load(sys.stdin)["Use"]]'); do
  echo "== build $d =="; ( cd "$d" && go build ./... ) || { echo "FAILED: $d"; break; }
done
```
Expected: every module prints its `== build … ==` header and exits cleanly; no `FAILED:` line.

- [ ] **Step 6: Verify core dependency isolation (the point of the whole change)**

Run:
```bash
go list -m all | grep -E 'gofiber|temporal|jackc/pgx|google.golang.org/grpc|opentelemetry' || echo "CORE CLEAN"
```
Expected: `CORE CLEAN` (none of the heavy deps remain in core's module graph).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum go.work go.work.sum pkg cmd examples
git commit -m "chore(modules): tidy per-pkg modules and trim core dependencies"
```

---

## Task 4: Make cmd/doccheck workspace-aware (HIGH-risk fix)

`doccheck` builds doc snippets in a throwaway temp module that today `require`/`replace`s **only** core and copies the root `go.sum`. Post-split, snippets importing an integration (e.g. `pkg/logging/tint`) point at a separate module the single replace does not cover, so they fail to compile. Fix: emit a `require`+`replace` for core **and every `pkg/` module**, and drop the stale `go.sum` copy (let `go build -mod=mod` populate it from the local replaces).

**Files:**
- Modify: `cmd/doccheck/main.go:73-88` (replace `setupTempModule`, drop `copyFile` usage for go.sum)
- Test: run `mise run doccheck` (green gate)

- [ ] **Step 1: Reproduce the failure (RED)**

Run:
```bash
mise run doccheck
```
Expected: FAIL — at least one snippet importing a `pkg/...` integration reports a compile/resolution error (the temp module cannot see the integration module). If it unexpectedly passes, confirm a doc snippet imports an integration with `grep -rn 'imports=.*pkg/' docs/` before proceeding; the fix is still required for correctness.

- [ ] **Step 2: Replace `setupTempModule` with a workspace-aware version**

In `cmd/doccheck/main.go`, add the import `"golang.org/x/mod/modfile"` to the import block, and replace the `setupTempModule` function (lines 73-88) with:

```go
func setupTempModule(tmpDir, repoRoot string) error {
	mods, err := discoverModuleDirs(repoRoot)
	if err != nil {
		return fmt.Errorf("discover modules: %w", err)
	}

	var b strings.Builder

	b.WriteString("module doccheck_snippet\n\ngo 1.26\n\n")

	for _, m := range mods {
		fmt.Fprintf(&b, "require %s v0.0.0\n", m.path)
	}

	b.WriteString("\n")

	for _, m := range mods {
		fmt.Fprintf(&b, "replace %s => %s\n", m.path, m.dir)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(b.String()), filePerm); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	return nil
}

type moduleDir struct {
	path string
	dir  string
}

// discoverModuleDirs returns the core module and every nested module under pkg/,
// so doc snippets can import any integration module, not just core.
func discoverModuleDirs(repoRoot string) ([]moduleDir, error) {
	corePath, err := readModulePath(repoRoot)
	if err != nil {
		return nil, err
	}

	mods := []moduleDir{{path: corePath, dir: repoRoot}}

	walkErr := filepath.WalkDir(filepath.Join(repoRoot, "pkg"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || d.Name() != "go.mod" {
			return nil
		}

		dir := filepath.Dir(p)

		mp, readErr := readModulePath(dir)
		if readErr != nil {
			return readErr
		}

		mods = append(mods, moduleDir{path: mp, dir: dir})

		return nil
	})

	return mods, walkErr
}

func readModulePath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("read go.mod in %s: %w", dir, err)
	}

	path := modfile.ModulePath(data)
	if path == "" {
		return "", fmt.Errorf("no module path in %s/go.mod", dir)
	}

	return path, nil
}
```

- [ ] **Step 3: Remove the now-unused `copyFile` function**

`copyFile` (lines 90-110) is only called by the old `setupTempModule`. Delete the `copyFile` function and its `io` import if `io` becomes unused (run `go build ./...` in `cmd` — it will report the unused import to remove).

- [ ] **Step 4: Verify the fix (GREEN)**

Run:
```bash
( cd cmd && go build ./... )
mise run doccheck
```
Expected: build PASS; `doccheck` PASS on all snippets including those importing integrations.

- [ ] **Step 5: Commit**

```bash
git add cmd/doccheck/main.go cmd/go.mod cmd/go.sum
git commit -m "fix(doccheck): build doc snippets against all pkg modules via per-module replace"
```

---

## Task 5: Fix repo-root discovery for genmodules/apicheck (multi-module)

`genmodules` and `apicheck` parse `.go` source via `go/parser` (they do **not** build), so they are unaffected by module boundaries — *provided* `findRepoRoot` resolves to the true repo root. Their `findRepoRoot` walks **up to the first `go.mod`**, which broke once Task 2 added `cmd/go.mod`: `docgen-check` runs `go generate ./cmd/docgen`, which sets cwd to `cmd/docgen/`, so `findRepoRoot` now stops at `cmd/go.mod` and mistakes `cmd/` for the root. Pinning the mise task `dir` does **not** fix this — `go generate` re-roots to the package dir regardless. The fix is to make `findRepoRoot` prefer the **workspace root** (`go.work` exists only at the true root), falling back to the first `go.mod` when no workspace is present. Pinning the mise `dir` is kept as belt-and-suspenders.

**Second regression (found during execution):** `cmd/docgen`'s `parseGoMod` read only the root `go.mod`. Post-split, third-party versions used for passthrough docs (e.g. `gofiber/fiber/v3` now in `pkg/http/fiber/go.mod`) vanished from `docs.yaml`, so `docgen-check` failed even after the `findRepoRoot` fix. `parseGoMod` must be made workspace-aware: walk up for `go.work`, merge `require` versions across every `use` module (falling back to the single root `go.mod` when no workspace).

**Files:**
- Modify: `cmd/genmodules/main.go` (`findRepoRoot`) and `cmd/apicheck/main.go` (identical copy)
- Modify: `cmd/docgen/main.go` (`parseGoMod` → workspace-aware version merge)
- Modify: `mise.toml` (the `docgen`, `docgen-check`, `apicheck`, `doccheck` task definitions)

- [ ] **Step 0: Make `findRepoRoot` prefer the workspace root**

In BOTH `cmd/genmodules/main.go` and `cmd/apicheck/main.go`, replace `findRepoRoot` with a version that first walks up looking for `go.work`, then falls back to the first `go.mod`:

```go
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	// Prefer the workspace root: go.work exists only at the true repo root, so a
	// nested module go.mod (e.g. cmd/go.mod) is not mistaken for the root when this
	// tool is invoked via `go generate` from a package subdirectory.
	for d := dir; ; {
		if _, statErr := os.Stat(filepath.Join(d, "go.work")); statErr == nil {
			return d, nil
		}

		parent := filepath.Dir(d)
		if parent == d {
			break
		}

		d = parent
	}

	// Fall back to the first go.mod walking up (no workspace present).
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found from working directory")
		}

		dir = parent
	}
}
```

- [ ] **Step 1: Inspect the current doc/api task definitions**

Run:
```bash
sed -n '/\[tasks.docgen\]/,/\[tasks.apicheck\]/p' mise.toml; echo '---'; sed -n '/\[tasks.apicheck\]/,/^$/p' mise.toml
```
Expected: shows the `run = "go run ./cmd/..."` lines for docgen/docgen-check/doccheck/apicheck.

- [ ] **Step 2: Pin each task to the repo root**

For each of `[tasks.docgen]`, `[tasks.docgen-check]`, `[tasks.doccheck]`, `[tasks.apicheck]` in `mise.toml`, add a `dir` key set to the config root if not already implicit:

```toml
[tasks.docgen]
dir = "{{ config_root }}"
run = "go run ./cmd/genmodules ..."   # keep existing run line unchanged
```
(Apply the same `dir = "{{ config_root }}"` line to `docgen-check`, `doccheck`, and `apicheck`. mise runs tasks from `config_root` by default, so this is belt-and-suspenders making the root explicit.)

- [ ] **Step 3: Verify the doc/api pipeline is green from root**

Run:
```bash
mise run docgen-check && mise run apicheck
```
Expected: both PASS (`apicheck: API index is up to date`; docgen output matches committed `docs.yaml`).

- [ ] **Step 4: Commit**

```bash
git add mise.toml
git commit -m "chore(docs): pin doc/api tooling tasks to repo root for multi-module layout"
```

---

## Task 6: Add mise tasks for ci-all, verify-isolation, and release

**Files:**
- Modify: `mise.toml`
- Create: `mise-tasks/release` (script) — or inline `run` in `mise.toml`

- [ ] **Step 1: Generate the core dependency golden file**

The spec mandates core isolation be guarded by an **allowlist (golden file)**, not a denylist, so a *new* heavy dependency leaking into core is caught — not only today's known ones.

Run (from repo root, after core was trimmed in Task 3):
```bash
go list -m all | awk '{print $1}' | sort > core-deps.golden
wc -l core-deps.golden   # sanity: only the light core set + transitive
```
Expected: `core-deps.golden` lists core's module + light deps (conc, do, koanf*, oops, slox, fsnotify, pflag) and their transitive deps — no gofiber/temporal/pgx/grpc/opentelemetry lines.

- [ ] **Step 2: Add a `verify-isolation` task**

Add to `mise.toml`:

```toml
[tasks.verify-isolation]
dir = "{{ config_root }}"
description = "Core deps match the golden allowlist; integrations carry no sibling heavy deps"
run = """
set -e
# Core: allowlist via golden file — catches ANY new dep entering core.
go list -m all | awk '{print $1}' | sort > "${TMPDIR:-/tmp}/lakta-core-deps.now"
if ! diff -u core-deps.golden "${TMPDIR:-/tmp}/lakta-core-deps.now"; then
  echo "CORE DEPS DRIFT: a dependency entered/left core. If intentional, regenerate core-deps.golden and review the diff."; exit 1
fi
# Integrations: denylist of sibling heavy deps.
check() { # dir, regex-of-forbidden
  if ( cd "$1" && go list -m all ) | grep -Eq "$2"; then
    echo "ISOLATION FAIL: $1 pulls $2"; exit 1
  fi
}
check pkg/http/fiber        'go.temporal|jackc/pgx|google.golang.org/grpc'
check pkg/logging/tint      'opentelemetry|gofiber'
check pkg/db/drivers/pgx    'gofiber|go.temporal|google.golang.org/grpc'
echo "isolation OK"
"""
```
The golden file is regenerated deliberately (reviewed diff) when core's deps legitimately change.

- [ ] **Step 3: Add a `ci-all` task that iterates workspace modules**

Add to `mise.toml`:

```toml
[tasks.ci-all]
dir = "{{ config_root }}"
description = "Build, test, race, lint, vuln-check every workspace module"
run = """
set -e
mods=$(go work edit -json | python3 -c 'import json,sys;[print(u["DiskPath"]) for u in json.load(sys.stdin)["Use"]]')
for d in $mods; do
  echo "== $d =="
  ( cd "$d" && go build ./... && go test ./... && go test -race ./... )
  ( cd "$d" && golangci-lint run )
  ( cd "$d" && govulncheck ./... )
done
"""
```

- [ ] **Step 4: Add a `release` task (synchronized, all tags on one commit)**

Create `mise-tasks/release` (executable) — usage `mise run release v0.0.7`.

**Why this is more than "pin core":** Task 3 left every published module with bootstrap `replace github.com/Vilsol/lakta… => <local dir>` directives plus a zero pseudo-version require (`v0.0.0-0001…`) on core and, for `pkg/grpc`/`pkg/http/fiber`, on `pkg/health`; `pkg/testkit` is required by several. Replace directives in a *dependency* are ignored by consumers — but the unresolvable zero-pseudo-version requires they mask are NOT. So the release must, for every published module: **drop the in-repo replaces** and **pin every intra-repo require to the release version** (core, health, testkit — not just core). Do this with `go mod edit` only; do **not** `go mod tidy` before the tags exist (resolution would fail). `cmd/` and `examples/` keep their replaces — they are never published and always build from the workspace.

```bash
#!/usr/bin/env bash
set -euo pipefail
VERSION="${1:?usage: release vX.Y.Z}"
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# Published modules only (NOT cmd/examples).
MODS=(. pkg/logging/tint pkg/logging/slog pkg/otel pkg/health \
      pkg/http/fiber pkg/grpc pkg/db/sql pkg/db/drivers/pgx \
      pkg/workflows/temporal pkg/testkit)

lakta_paths() { # field: Replace|Require -> list lakta module paths in this module's go.mod
  go mod edit -json | python3 -c "
import json,sys
d=json.load(sys.stdin); f='$1'
items = (d.get('Replace') or []) if f=='Replace' else (d.get('Require') or [])
for it in items:
    p = it['Old']['Path'] if f=='Replace' else it['Path']
    if p.startswith('github.com/Vilsol/lakta'): print(p)
"
}

# 1. Drop in-repo replaces and pin every intra-repo require to VERSION (go mod edit only).
for d in "${MODS[@]}"; do
  (
    cd "$d"
    for r in $(lakta_paths Replace); do go mod edit -dropreplace="$r"; done
    for q in $(lakta_paths Require); do go mod edit -require="${q}@${VERSION}"; done
  )
done
git commit -am "chore(release): pin lakta requires to ${VERSION}, drop bootstrap replaces"

# 2. Build the tag list (subdir-prefixed for nested modules).
TAGS=("${VERSION}")
for d in "${MODS[@]}"; do
  [ "$d" = "." ] && continue
  TAGS+=("${d}/${VERSION}")
done

# 3. Tag all on the same commit, push together; roll back local tags on push failure.
for t in "${TAGS[@]}"; do git tag "$t"; done
if ! git push origin "${TAGS[@]}"; then
  echo "push failed — deleting locally created tags for clean retry" >&2
  for t in "${TAGS[@]}"; do git tag -d "$t" || true; done
  exit 1
fi

# 4. Post-push resolution verification, outside the workspace.
tmp="$(mktemp -d)"; ( cd "$tmp" && GOWORK=off GOFLAGS=-mod=mod \
  go list -m "github.com/Vilsol/lakta@${VERSION}" "github.com/Vilsol/lakta/pkg/http/fiber@${VERSION}" )
echo "release ${VERSION} complete and resolvable"
```

- [ ] **Step 5: Verify the new tasks parse and isolation passes**

Run:
```bash
mise tasks | grep -E 'ci-all|verify-isolation|release'
mise run verify-isolation
```
Expected: tasks listed; `isolation OK`.

- [ ] **Step 6: Commit**

```bash
chmod +x mise-tasks/release
git add mise.toml mise-tasks/release core-deps.golden
git commit -m "chore(modules): add ci-all, verify-isolation, release tasks and core deps golden"
```

---

## Task 7: Update CI workflow, Renovate, README, and CHANGELOG

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `renovate.json`
- Modify: `README.md`
- Create: `CHANGELOG.md`

- [ ] **Step 1: Point CI at the multi-module tasks**

In `.github/workflows/ci.yml`, replace the per-step `go build ./...`, `mise run test`, `go test -race ./...`, `mise run lint`, and `mise run govulncheck` steps with a single step:

```yaml
      - name: Build, test, lint, vuln (all modules)
        run: mise run ci-all

      - name: Dependency isolation
        run: mise run verify-isolation
```
Keep the existing `doccheck`, `docgen-check`, and `apicheck` steps as-is (they now run from root and resolve the workspace).

- [ ] **Step 2: Enable gomod tidy across modules in Renovate**

In `renovate.json`, add (merging with existing config):

```json
{
  "postUpdateOptions": ["gomodTidy"]
}
```

- [ ] **Step 3: Document per-module installation and the workspace flow in README**

Add a short section to `README.md` (outer fence is four backticks so the nested ```` ```bash ```` block renders intact):

````markdown
## Installation

Lakta is split into per-integration modules — install only what you use:

```bash
go get github.com/Vilsol/lakta@latest                    # core runtime + config
go get github.com/Vilsol/lakta/pkg/http/fiber@latest     # HTTP (Fiber)
go get github.com/Vilsol/lakta/pkg/workflows/temporal@latest
```

### Contributing

This is a multi-module workspace. After pulling changes that touch
dependencies, run `go work sync`. CI runs every module via `mise run ci-all`.
````

- [ ] **Step 4: Start a CHANGELOG**

Create `CHANGELOG.md`:

```markdown
# Changelog

All modules are versioned in lockstep; one entry per repo version.

## Unreleased

### Changed
- Split the framework into per-package modules. Import paths are unchanged
  (`pkg/` retained); integrations are now installed as separate modules so
  consumers pull only the dependencies they use.
```

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml renovate.json README.md CHANGELOG.md
git commit -m "ci(modules): run all modules in CI, document multi-module layout"
```

---

## Task 8: Full green gate and isolation acid test

**Files:** none (verification only)

- [ ] **Step 1: Run the full multi-module gate**

Run:
```bash
mise run ci-all
```
Expected: every module builds, tests pass, `-race` clean, lint clean, govulncheck clean.

- [ ] **Step 2: Run the doc/api pipeline**

Run:
```bash
mise run doccheck && mise run docgen-check && mise run apicheck
```
Expected: all PASS.

- [ ] **Step 3: Run the isolation acid test**

Run:
```bash
mise run verify-isolation
```
Expected: `isolation OK` (core matches `core-deps.golden`; no integration carries a sibling's heavy deps). A non-zero exit prints either `CORE DEPS DRIFT` (with a diff) or `ISOLATION FAIL: <dir> pulls <dep>`.

- [ ] **Step 4: Confirm import strings are unchanged (zero churn)**

Run:
```bash
git diff main --stat -- '*.go' | grep -vE 'cmd/doccheck/main.go' | grep -E '\.go ' || echo "no .go content changes outside doccheck"
```
Expected: `no .go content changes outside doccheck` (only `go.mod`/`go.work`/tooling changed; no integration import statements were rewritten).

- [ ] **Step 5: Open the PR**

```bash
git push -u origin worktree-split-modules
gh pr create --fill --title "Split lakta into per-pkg modules"
```
On merge, run `mise run release v0.0.7` and confirm the post-push resolution check.

---

## Notes for the implementer

- **Pseudo-versions during dev are expected.** Until Task 6's release runs, integration `go.mod`s may show `require github.com/Vilsol/lakta v0.0.0-…`. The workspace overrides this locally; do not hand-edit.
- **`go.work` and `go.work.sum` are committed** — they are ignored when modules are consumed externally, so this is safe and gives every contributor consistent local resolution.
- **If a module fails to build in Task 3**, the likely cause is an undeclared cross-module import (the design assumes a pure star — integrations import only core). Run `go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./...` in the failing module to find the stray import and confirm against the spec before adding a require.
