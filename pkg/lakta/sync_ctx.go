package lakta

import "context"

// SyncCtx can be embedded in SyncModule implementations to eliminate the
// runtimeContext field boilerplate and the manual assignment in Start.
// The runtime automatically injects the context before calling Start.
//
// Usage:
//
//	type Module struct {
//	    lakta.SyncCtx
//	    // ...
//	}
//
//	// In Init interceptors, read it lazily (it's set by the time requests arrive):
//	runtimeCtx := trace.ContextWithSpan(m.RuntimeCtx(), span)
type SyncCtx struct {
	ctx context.Context //nolint:containedctx
}

// RuntimeCtx returns the runtime context injected before Start is called.
func (s *SyncCtx) RuntimeCtx() context.Context { return s.ctx }

// contextSetter is detected by the runtime to inject ctx before Start.
type contextSetter interface {
	setCtx(ctx context.Context)
}

func (s *SyncCtx) setCtx(ctx context.Context) { s.ctx = ctx }
