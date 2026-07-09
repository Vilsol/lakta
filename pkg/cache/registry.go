package cache

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/samber/oops"
)

// box is the untyped ([any,any]) cache handle a backend materializes. The typed
// Cache[K,V] returned by Named boxes keys/values across this seam so a
// config-declared cache — whose K,V are unknown until first use — can be built
// without instantiating backend generics from a reflect.Type (impossible in Go).
type box interface {
	Get(key any) (any, bool)
	Set(key, value any)
	Delete(key any)
	GetOrLoad(ctx context.Context, key any, load func(context.Context, any) (any, error)) (any, error)
	Stats() Stats
}

// Builder materializes a backend box for a declared cache name. The backend
// registers one per cache; the (K,V) reflect types recorded at the first Named
// call are passed through for backends that want them (memory boxes as any/any).
type Builder func(keyType, valType reflect.Type) (any, error)

// entry is one registered cache: its builder, the memoized box + typed handle
// once bound, and the recorded (K,V) types for the identity guard.
type entry struct {
	build   Builder
	box     box
	typed   any
	keyType reflect.Type
	valType reflect.Type
}

// Registry holds declared/built caches by name with a reflect type-identity
// guard. Populated by a backend module's Init; consumed by Named during app
// modules' Init (topo-sort trick, as pool/scheduler do).
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*entry)}
}

// Register declares a cache name with its Builder. A second Register for the
// same name replaces the declaration (unbinding any previous build).
func (r *Registry) Register(name string, build Builder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[name] = &entry{build: build}
}

// Unregister drops a cache name from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

// Stats returns a per-name snapshot for actuator introspection. Only bound
// caches (already materialized via Named) report; unbound declarations are
// omitted.
func (r *Registry) Stats() map[string]Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]Stats, len(r.entries))
	for name, e := range r.entries {
		if e.box != nil {
			out[name] = e.box.Stats()
		}
	}
	return out
}

// typedCache boxes a backend box behind the typed Cache[K,V] interface.
type typedCache[K comparable, V any] struct {
	b box
}

func (c typedCache[K, V]) Get(key K) (V, bool) { //nolint:ireturn // V is a generic type param, not an interface return
	v, ok := c.b.Get(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true //nolint:forcetypeassert // the identity guard ensures K,V match
}

func (c typedCache[K, V]) Set(key K, value V) { c.b.Set(key, value) }

func (c typedCache[K, V]) Delete(key K) { c.b.Delete(key) }

func (c typedCache[K, V]) GetOrLoad(ctx context.Context, key K, load Loader[K, V]) (V, error) { //nolint:ireturn // V is a generic type param
	v, err := c.b.GetOrLoad(ctx, key, func(ctx context.Context, k any) (any, error) {
		return load(ctx, k.(K)) //nolint:forcetypeassert // key is always the boxed K
	})
	if err != nil {
		var zero V
		return zero, err //nolint:wrapcheck // propagate the caller's loader error unchanged
	}
	return v.(V), nil //nolint:forcetypeassert // the identity guard ensures K,V match
}

func (c typedCache[K, V]) Stats() Stats { return c.b.Stats() }

// Named lazily binds (K,V) to a declared cache name and memoizes the result.
// The first call records reflect.TypeFor[K]/[V] and materializes the cache via
// the entry's Builder; repeat calls with the same name return the same handle.
// A call with the same name but a different K or V returns a type-mismatch error
// naming both the recorded and requested (K,V). An unknown name returns a
// diagnostic not-found error listing the known names.
func Named[K comparable, V any](reg *Registry, name string) (Cache[K, V], error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	e, ok := reg.entries[name]
	if !ok {
		known := slices.Sorted(maps.Keys(reg.entries))
		return nil, oops.Errorf(
			"cache %q not registered — declare it under modules.cache.memory.<inst>.caches or via WithCache; known: %v",
			name, known,
		)
	}

	kt, vt := reflect.TypeFor[K](), reflect.TypeFor[V]()

	if e.box != nil {
		if e.keyType != kt || e.valType != vt {
			return nil, oops.Errorf(
				"cache %q already bound to (%s, %s); cannot rebind to (%s, %s)",
				name, e.keyType, e.valType, kt, vt,
			)
		}
		return e.typed.(Cache[K, V]), nil //nolint:forcetypeassert // types verified above
	}

	built, err := e.build(kt, vt)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to build cache %q", name)
	}

	b, ok := built.(box)
	if !ok {
		return nil, oops.Errorf("cache %q builder returned %T which is not a cache backend", name, built)
	}

	typed := typedCache[K, V]{b: b}
	e.box, e.typed, e.keyType, e.valType = b, typed, kt, vt
	return typed, nil
}
