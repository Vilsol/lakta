# Changelog

All modules are versioned in lockstep; one entry per repo version.

## Unreleased

### Changed
- Split the framework into per-package modules. Import paths are unchanged
  (`pkg/` retained); integrations are now installed as separate modules so
  consumers pull only the dependencies they use.

### Known limitations
- Modules using `pkg/health` (which wraps `hellofresh/health-go`) inherit
  health-go's bundled optional checker modules (pgx, grpc, …) in their *module*
  graph / `go.sum`, even though those packages are never compiled into your
  binary. This is a property of health-go, not the split; the package graph
  stays isolated (verified by `mise run verify-isolation`).
