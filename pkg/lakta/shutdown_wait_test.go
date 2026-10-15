package lakta_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
)

// slowStopper's Start keeps running for a while after ctx is cancelled, like a
// worker draining in-flight tasks.
type slowStopper struct {
	startReturned atomic.Int64
	shutdownAt    atomic.Int64
}

func (s *slowStopper) Init(context.Context) error { return nil }

func (s *slowStopper) Start(ctx context.Context) error {
	<-ctx.Done()
	time.Sleep(200 * time.Millisecond)
	s.startReturned.Store(time.Now().UnixNano())
	return nil
}

func (s *slowStopper) Shutdown(context.Context) error {
	s.shutdownAt.Store(time.Now().UnixNano())
	return nil
}

func TestRunContext_SignalShutdownWaitsForStartToReturn(t *testing.T) {
	t.Parallel()

	m := &slowStopper{}
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	testza.AssertNil(t, lakta.NewRuntime(m).RunContext(ctx))
	testza.AssertNotEqual(t, int64(0), m.startReturned.Load())
	testza.AssertTrue(t, m.shutdownAt.Load() >= m.startReturned.Load(), "Shutdown ran before Start returned")
}
