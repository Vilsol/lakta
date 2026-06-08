package lakta

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
)

type blockingShutdownModule struct {
	release chan struct{}
}

func (blockingShutdownModule) Init(context.Context) error       { return nil }
func (m blockingShutdownModule) Shutdown(context.Context) error { <-m.release; return nil }

type quickModule struct{ err error }

func (quickModule) Init(context.Context) error       { return nil }
func (m quickModule) Shutdown(context.Context) error { return m.err }

type panicModule struct{}

func (panicModule) Init(context.Context) error     { return nil }
func (panicModule) Shutdown(context.Context) error { panic("shutdown boom") }

func TestShutdownModule_ReturnsOnDeadline(t *testing.T) {
	t.Parallel()

	m := blockingShutdownModule{release: make(chan struct{})}
	t.Cleanup(func() { close(m.release) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := shutdownModule(ctx, m)

	testza.AssertNotNil(t, err)
	testza.AssertTrue(t, time.Since(start) < time.Second, "shutdownModule must return near the deadline, not block on Shutdown")
}

func TestShutdownModule_ReturnsResultWhenFast(t *testing.T) {
	t.Parallel()

	testza.AssertNil(t, shutdownModule(context.Background(), quickModule{}))

	wantErr := errors.New("x")
	testza.AssertNotNil(t, shutdownModule(context.Background(), quickModule{err: wantErr}))
}

func TestShutdownModule_RecoversPanic(t *testing.T) {
	t.Parallel()

	err := shutdownModule(context.Background(), panicModule{})

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "shutdown boom")
}

func TestSafeCall_RecoversPanic(t *testing.T) {
	t.Parallel()

	err := safeCall(func() error { panic("boom") })

	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "boom")
}
