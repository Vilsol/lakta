// Package cache is the backend-agnostic caching seam for lakta. It defines the
// typed Cache[K,V] interface, a Registry of config-declared named caches, and
// the Named[K,V] resolver that binds concrete key/value types to a declared
// cache on first use.
//
// The core lives in the root module and pulls no heavy dependencies (stdlib +
// reflect only), so consumers get cache.Named/cache.Memoize without importing a
// backend. A backend (e.g. pkg/cache/memory on otter) registers Builders into a
// Registry during its Init and provides the Registry via DI.
//
// Lazy type binding: config declares a cache's sizing by name, but the concrete
// (K,V) is unknown until an app module calls Named[K,V]. The first Named call
// records the (K,V) reflect identity and materializes the cache; a later call
// for the same name with a different K or V returns an error rather than
// silently rebuilding, so a config-declared cache stays type-safe.
package cache
