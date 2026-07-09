package cache

import "context"

// Loader loads a value for a key on cache miss. Used by GetOrLoad and Memoize.
type Loader[K comparable, V any] func(ctx context.Context, key K) (V, error)

// Cache is the backend-agnostic seam. memory (otter) and future backends back it.
type Cache[K comparable, V any] interface {
	Get(key K) (V, bool)
	Set(key K, value V)
	Delete(key K)
	// GetOrLoad returns the cached value or invokes load exactly once across
	// concurrent callers for the same key (dogpile-safe / singleflight).
	GetOrLoad(ctx context.Context, key K, load Loader[K, V]) (V, error)
	Stats() Stats
}

// Stats is the flat, backend-free introspection record read by Registry.Stats.
type Stats struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
	Size      int
}

// Memoize adapts a Cache + Loader into a plain func that calls GetOrLoad.
func Memoize[K comparable, V any](c Cache[K, V], load Loader[K, V]) func(context.Context, K) (V, error) {
	return func(ctx context.Context, key K) (V, error) {
		return c.GetOrLoad(ctx, key, load)
	}
}
