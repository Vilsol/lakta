package lakta

import (
	"context"
)

type Module interface {
	Init(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type SyncModule interface {
	Module
	Start(ctx context.Context) error
}

type AsyncModule interface {
	Module
	StartAsync(ctx context.Context) error
}
