package temporal

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"go.temporal.io/sdk/client"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Provider     = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

func TestModule_Provides_ClientType(t *testing.T) {
	t.Parallel()

	provides := NewModule().Provides()
	testza.AssertEqual(t, []reflect.Type{reflect.TypeFor[client.Client]()}, provides)
}

func TestModule_SignalStop_ClosesInterruptCh(t *testing.T) {
	t.Parallel()

	m := NewModule()
	m.signalStop()

	select {
	case <-m.interruptCh:
	case <-time.After(time.Second):
		t.Fatal("interruptCh was not closed by signalStop")
	}
}

func TestModule_SignalStop_Idempotent(t *testing.T) {
	t.Parallel()

	m := NewModule()
	m.signalStop()
	testza.AssertNotPanics(t, func() { m.signalStop() })
}

func TestModule_CloseClient_NoClientNoPanic(t *testing.T) {
	t.Parallel()

	m := NewModule()
	testza.AssertNotPanics(t, func() { m.closeClient() })
	testza.AssertNotPanics(t, func() { m.closeClient() })
}

func TestModule_Shutdown_Idempotent(t *testing.T) {
	t.Parallel()

	m := NewModule()
	testza.AssertNotPanics(t, func() {
		_ = m.Shutdown(context.Background())
		_ = m.Shutdown(context.Background())
	})
}

func TestModule_CtxCancel_ClosesInterruptCh(t *testing.T) {
	t.Parallel()

	m := NewModule()
	ctx, cancel := context.WithCancel(context.Background())

	// Mirror the ctx watcher goroutine started in Start.
	go func() {
		select {
		case <-ctx.Done():
			m.signalStop()
		case <-m.interruptCh:
		}
	}()

	cancel()

	select {
	case <-m.interruptCh:
	case <-time.After(time.Second):
		t.Fatal("interruptCh was not closed on context cancellation")
	}
}
