package fiberserver

import (
	"slices"
	"sync"

	"github.com/gofiber/fiber/v3"
)

// RoutesSnapshot is one fiber instance's routes, tagged by instance name.
type RoutesSnapshot struct {
	Instance string
	Routes   []fiber.Route
}

// RoutesRegistry is the single shared, concurrency-safe aggregator of every
// fiber instance's routes, so consumers (e.g. the actuator) can expose routes
// across all instances while each *fiber.App stays private. One instance lives
// in DI; each fiber module provides-if-absent then Append()s its snapshot at
// Start.
type RoutesRegistry struct {
	mu   sync.Mutex
	snap []RoutesSnapshot
}

// NewRoutesRegistry returns an empty registry.
func NewRoutesRegistry() *RoutesRegistry {
	return &RoutesRegistry{}
}

// Append records one instance's snapshot under the lock.
func (r *RoutesRegistry) Append(s RoutesSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap = append(r.snap, s)
}

// Snapshot returns an independent copy of all recorded snapshots under the lock.
func (r *RoutesRegistry) Snapshot() []RoutesSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]RoutesSnapshot, len(r.snap))
	for i, s := range r.snap {
		out[i] = RoutesSnapshot{Instance: s.Instance, Routes: slices.Clone(s.Routes)}
	}
	return out
}
