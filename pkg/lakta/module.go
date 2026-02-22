package lakta

import (
	"context"
	"reflect"
)

// Module is the base interface for all lakta modules.
type Module interface {
	Init(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// SyncModule extends Module with a blocking Start method for long-running services.
type SyncModule interface {
	Module
	Start(ctx context.Context) error
}

// AsyncModule extends Module with a non-blocking StartAsync method for background tasks.
type AsyncModule interface {
	Module
	StartAsync(ctx context.Context) error
}

// Provider declares what types this module registers in DI.
// The runtime uses this to build the dependency graph for Init ordering.
type Provider interface {
	Provides() []reflect.Type
}

// Dependent declares what types this module needs from DI before Init runs.
// Required types with no registered provider cause a startup error before any Init fires.
// Optional types with no registered provider are silently skipped.
type Dependent interface {
	Dependencies() (required, optional []reflect.Type)
}
