package cache

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/MarvinJWendt/testza"
)

// fakeBox is an in-memory box for exercising the core registry without a backend.
type fakeBox struct {
	mu    sync.Mutex
	data  map[any]any
	stats Stats
}

func newFakeBox() *fakeBox { return &fakeBox{data: make(map[any]any)} }

func (b *fakeBox) Get(key any) (any, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.data[key]
	return v, ok
}

func (b *fakeBox) Set(key, value any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = value
}

func (b *fakeBox) Delete(key any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.data, key)
}

func (b *fakeBox) GetOrLoad(ctx context.Context, key any, load func(context.Context, any) (any, error)) (any, error) {
	b.mu.Lock()
	if v, ok := b.data[key]; ok {
		b.mu.Unlock()
		return v, nil
	}
	b.mu.Unlock()
	v, err := load(ctx, key)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.data[key] = v
	b.mu.Unlock()
	return v, nil
}

func (b *fakeBox) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.stats
	s.Size = len(b.data)
	return s
}

func fakeRegistry(t *testing.T, names ...string) *Registry {
	t.Helper()
	reg := NewRegistry()
	for _, name := range names {
		reg.Register(name, func(_, _ reflect.Type) (any, error) { return newFakeBox(), nil })
	}
	return reg
}

func TestNamed_TypeIdentityGuard(t *testing.T) {
	t.Parallel()
	reg := fakeRegistry(t, "sessions")

	c1, err := Named[string, int](reg, "sessions")
	testza.AssertNoError(t, err)
	testza.AssertNotNil(t, c1)

	_, err = Named[string, string](reg, "sessions")
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "string, int")
	testza.AssertContains(t, err.Error(), "string, string")

	c2, err := Named[string, int](reg, "sessions")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, c1, c2) // same memoized handle
}

func TestNamed_UnknownNameDiagnostic(t *testing.T) {
	t.Parallel()
	reg := fakeRegistry(t, "sessions", "tokens")

	_, err := Named[string, int](reg, "missing")
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "not registered")
	testza.AssertContains(t, err.Error(), "sessions")
	testza.AssertContains(t, err.Error(), "tokens")
}

func TestTypedCache_GetSetDelete(t *testing.T) {
	t.Parallel()
	reg := fakeRegistry(t, "c")
	c, err := Named[string, int](reg, "c")
	testza.AssertNoError(t, err)

	_, ok := c.Get("a")
	testza.AssertFalse(t, ok)

	c.Set("a", 42)
	v, ok := c.Get("a")
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 42, v)

	c.Delete("a")
	_, ok = c.Get("a")
	testza.AssertFalse(t, ok)
}

func TestMemoize_CallsGetOrLoad(t *testing.T) {
	t.Parallel()
	reg := fakeRegistry(t, "c")
	c, err := Named[string, int](reg, "c")
	testza.AssertNoError(t, err)

	var calls int
	load := func(_ context.Context, key string) (int, error) {
		calls++
		return len(key), nil
	}
	fn := Memoize(c, load)

	v, err := fn(t.Context(), "abc")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 3, v)

	v, err = fn(t.Context(), "abc")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, 3, v)
	testza.AssertEqual(t, 1, calls) // second call served from cache
}

func TestRegistry_StatsOnlyBound(t *testing.T) {
	t.Parallel()
	reg := fakeRegistry(t, "bound", "unbound")

	c, err := Named[string, int](reg, "bound")
	testza.AssertNoError(t, err)
	c.Set("x", 1)

	stats := reg.Stats()
	_, ok := stats["bound"]
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 1, stats["bound"].Size)
	_, ok = stats["unbound"]
	testza.AssertFalse(t, ok) // never Named -> not reported
}
