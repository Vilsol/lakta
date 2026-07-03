package pool

import (
	"context"
	"maps"
	"slices"

	"github.com/samber/oops"
)

// Registry holds the named pools defined in config and code options.
type Registry struct {
	pools map[string]*Pool
}

// Get returns the named pool, or an error listing the known pool names.
func (r *Registry) Get(name string) (*Pool, error) {
	p, ok := r.pools[name]
	if !ok {
		known := slices.Sorted(maps.Keys(r.pools))
		return nil, oops.Errorf("unknown worker pool %q (known pools: %v)", name, known)
	}
	return p, nil
}

// close signals all pools to stop first so they drain concurrently, then
// waits for each, bounded by ctx.
func (r *Registry) close(ctx context.Context) error {
	for _, p := range r.pools {
		p.beginClose()
	}
	for _, p := range r.pools {
		if err := p.awaitClose(ctx); err != nil {
			return err
		}
	}
	return nil
}
