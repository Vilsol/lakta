package lakta

import (
	"context"
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
