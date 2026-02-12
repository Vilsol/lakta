package slog

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/Vilsol/lakta/pkg/lakta"
	slogotel "github.com/remychantenay/slog-otel"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	slogmulti "github.com/samber/slog-multi"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

var _ lakta.Module = (*Module)(nil)

type Module struct {
	logger       *slog.Logger
	runtimeFrame runtime.Frame
}

func NewModule() *Module {
	// Resolve caller
	var pcs [32]uintptr
	runtime.Callers(2, pcs[:])
	fs := runtime.CallersFrames(pcs[:])
	f, _ := fs.Next()

	// Bubble up to main
	for f.Function != "main.main" {
		f, _ = fs.Next()
	}

	return &Module{
		runtimeFrame: f,
	}
}

func (m *Module) Init(ctx context.Context) error {
	handler, err := do.Invoke[slog.Handler](lakta.GetInjector(ctx))
	if err != nil {
		return oops.Wrapf(err, "failed to retrieve logger handler")
	}

	m.logger = slog.New(
		newStackRewriter(ctx, slogmulti.Fanout(
			slogotel.New(handler),

			// TODO Configurable name
			otelslog.NewHandler("lakta"),
		), m.runtimeFrame),
	)

	lakta.Provide(ctx, m.GetLogger)

	return nil
}

func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

func (m *Module) GetLogger(_ do.Injector) (*slog.Logger, error) {
	return m.logger, nil
}
