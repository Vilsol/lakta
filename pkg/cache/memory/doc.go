// Package memory is an in-process cache backend for lakta built on
// maypok86/otter v2 (adaptive W-TinyLFU). It materializes config-declared named
// caches behind the backend-agnostic cache.Cache[K,V] seam, with dogpile-safe
// GetOrLoad, per-entry TTL, optional otel hit/miss/eviction stats, and
// hot-reload (live max-size resize; TTL change rebuilds and drops entries).
//
// Caches are declared by sizing under modules.cache.memory.<inst>.caches (or via
// WithCache) and bound to concrete types on first cache.Named[K,V] call. Because
// Go cannot instantiate a generic otter cache from a runtime reflect.Type, each
// otter cache is otter.Cache[any, any] and the typed handle boxes keys/values;
// the registry's (K,V) identity guard keeps the boxed assertions safe.
package memory
