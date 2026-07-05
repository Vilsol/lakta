package lakta

import (
	"reflect"
	"slices"
	"sync"
	"time"
)

// LifecycleKind classifies how the runtime drives a module: it blocks in Start
// (sync), runs a background StartAsync, or is init-only. Derived from which of
// SyncModule/AsyncModule the concrete module implements.
type LifecycleKind int

const (
	LifecycleInit  LifecycleKind = iota // implements neither SyncModule nor AsyncModule
	LifecycleSync                       // implements SyncModule
	LifecycleAsync                      // implements AsyncModule
)

func (k LifecycleKind) String() string {
	switch k {
	case LifecycleSync:
		return "sync"
	case LifecycleAsync:
		return "async"
	case LifecycleInit:
		return "init"
	default:
		return "unknown"
	}
}

// ModuleState is the furthest lifecycle stage a module has reached. It mutates
// through boot and shutdown from multiple goroutines; read only via Snapshot.
type ModuleState int

const (
	StatePending     ModuleState = iota // captured, not yet initialized
	StateInitialized                    // Init returned nil
	StateStarted                        // Start/StartAsync goroutine entered
	StateStopped                        // Shutdown returned
	StateFailed                         // Init returned an error
)

func (s ModuleState) String() string {
	switch s {
	case StateInitialized:
		return "initialized"
	case StateStarted:
		return "started"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	case StatePending:
		return "pending"
	default:
		return "unknown"
	}
}

// ModuleInfo is the captured metadata for one module, indexed by topo (init)
// order. Provides/Requires/Optional are the reflect.Type slices already
// computed by sortModules, rendered to strings for transport/rendering.
type ModuleInfo struct {
	Name         string // NamedModule.Name() if implemented, else ""
	Type         string // fmt.Sprintf("%T", module)
	InitOrder    int    // 0-based position in the topo-sorted slice
	Provides     []string
	Requires     []string
	Optional     []string
	Lifecycle    LifecycleKind
	State        ModuleState
	InitDuration time.Duration
}

// RuntimeInfo is the live registry of module metadata the runtime populates
// during RunContext and provides into DI. State fields mutate under mu across
// lifecycle goroutines — consumers must call Snapshot at read time and never
// cache the returned slice.
type RuntimeInfo struct {
	mu      sync.Mutex
	modules []ModuleInfo // indexed by InitOrder
}

// Snapshot returns an independent deep copy taken under lock: mutating the
// result (or a later lifecycle transition) does not affect prior snapshots.
func (ri *RuntimeInfo) Snapshot() []ModuleInfo {
	ri.mu.Lock()
	defer ri.mu.Unlock()

	out := slices.Clone(ri.modules)
	for i := range out {
		out[i].Provides = slices.Clone(out[i].Provides)
		out[i].Requires = slices.Clone(out[i].Requires)
		out[i].Optional = slices.Clone(out[i].Optional)
	}
	return out
}

// setState records the furthest stage reached for the module at order. Nil
// receiver and out-of-range order are no-ops (best-effort start/stop capture).
func (ri *RuntimeInfo) setState(order int, s ModuleState) {
	if ri == nil {
		return
	}

	ri.mu.Lock()
	defer ri.mu.Unlock()

	if order < 0 || order >= len(ri.modules) {
		return
	}
	ri.modules[order].State = s
}

// setInitDuration records how long the module at order took to Init.
func (ri *RuntimeInfo) setInitDuration(order int, d time.Duration) {
	if ri == nil {
		return
	}

	ri.mu.Lock()
	defer ri.mu.Unlock()

	if order < 0 || order >= len(ri.modules) {
		return
	}
	ri.modules[order].InitDuration = d
}

// lifecycleOf classifies a module by the start interface it implements.
func lifecycleOf(m Module) LifecycleKind {
	switch m.(type) {
	case SyncModule:
		return LifecycleSync
	case AsyncModule:
		return LifecycleAsync
	default:
		return LifecycleInit
	}
}

// renderTypes maps a []reflect.Type to reflect.Type.String() forms.
func renderTypes(ts []reflect.Type) []string {
	if len(ts) == 0 {
		return nil
	}

	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.String()
	}
	return out
}
