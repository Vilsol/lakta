package lakta

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
)

func TestLifecycleKind_String(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "init", LifecycleInit.String())
	testza.AssertEqual(t, "sync", LifecycleSync.String())
	testza.AssertEqual(t, "async", LifecycleAsync.String())
}

func TestModuleState_String(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "pending", StatePending.String())
	testza.AssertEqual(t, "initialized", StateInitialized.String())
	testza.AssertEqual(t, "started", StateStarted.String())
	testza.AssertEqual(t, "stopped", StateStopped.String())
	testza.AssertEqual(t, "failed", StateFailed.String())
}

func TestRuntimeInfo_SnapshotIndependent(t *testing.T) {
	t.Parallel()

	const mutated = "mutated"

	info := &RuntimeInfo{modules: []ModuleInfo{{
		Type:     "*lakta.original",
		Provides: []string{"*lakta.depA"},
		Requires: []string{"*lakta.depB"},
		Optional: []string{"*lakta.depC"},
	}}}

	snap1 := info.Snapshot()
	snap1[0].Type = mutated
	snap1[0].Provides[0] = mutated
	snap1[0].Requires[0] = mutated
	snap1[0].Optional[0] = mutated

	snap2 := info.Snapshot()
	testza.AssertEqual(t, "*lakta.original", snap2[0].Type)
	testza.AssertEqual(t, "*lakta.depA", snap2[0].Provides[0])
	testza.AssertEqual(t, "*lakta.depB", snap2[0].Requires[0])
	testza.AssertEqual(t, "*lakta.depC", snap2[0].Optional[0])
}

func TestRuntimeInfo_SetStateOutOfRangeIsNoop(t *testing.T) {
	t.Parallel()

	info := &RuntimeInfo{modules: make([]ModuleInfo, 1)}
	info.setState(-1, StateStarted)
	info.setState(1, StateStarted)
	info.setInitDuration(-1, time.Second)
	info.setInitDuration(1, time.Second)

	testza.AssertEqual(t, StatePending, info.Snapshot()[0].State)
}

func TestRuntimeInfo_NilReceiverIsNoop(t *testing.T) {
	t.Parallel()

	var info *RuntimeInfo
	info.setState(0, StateStarted)
	info.setInitDuration(0, time.Second)
}

func TestRuntimeInfo_ConcurrentMutation(t *testing.T) {
	t.Parallel()

	const (
		writers    = 8
		iterations = 200
		moduleLen  = 4
	)

	info := &RuntimeInfo{modules: make([]ModuleInfo, moduleLen)}

	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for i := range iterations {
				info.setState(i%moduleLen, StateStarted)
				info.setInitDuration(i%moduleLen, time.Duration(i))
			}
		})
	}

	wg.Go(func() {
		for range iterations {
			_ = info.Snapshot()
		}
	})

	wg.Wait()

	snap := info.Snapshot()
	for i := range moduleLen {
		testza.AssertEqual(t, StateStarted, snap[i].State)
	}
}

func TestLifecycleOf(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, LifecycleInit, lifecycleOf(&declModule{}))
	testza.AssertEqual(t, LifecycleSync, lifecycleOf(&infoSyncModule{}))
	testza.AssertEqual(t, LifecycleAsync, lifecycleOf(&infoAsyncModule{}))
}

func TestRenderTypes(t *testing.T) {
	t.Parallel()

	testza.AssertNil(t, renderTypes(nil))
	testza.AssertEqual(t,
		[]string{"*lakta.depA", "lakta.depB"},
		renderTypes([]reflect.Type{reflect.TypeFor[*depA](), reflect.TypeFor[depB]()}),
	)
}
