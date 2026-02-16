package lakta

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vilsol/slox"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"github.com/sourcegraph/conc/pool"
)

const DefaultShutdownTimeout = 30 * time.Second

// Runtime orchestrates module initialization, startup, and shutdown.
type Runtime struct {
	modules []Module
}

// NewRuntime creates a runtime with the given modules (order matters for init).
func NewRuntime(modules ...Module) *Runtime {
	return &Runtime{
		modules: modules,
	}
}

// Run starts the runtime with a background context.
func (r *Runtime) Run() error {
	return r.RunContext(context.Background())
}

// RunContext initializes, starts, and manages graceful shutdown of all modules.
func (r *Runtime) RunContext(ctx context.Context) error {
	injector := do.New()
	ctx = WithInjector(ctx, injector)

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize modules sequentially in the order they were provided
	for _, module := range r.modules {
		if err := module.Init(shutdownCtx); err != nil {
			slox.Error(ctx, "failed initializing modules", slog.Any("error", err))
			return oops.
				With("name", fmt.Sprintf("%T", module)).
				Wrapf(err, "failed initializing module")
		}
	}

	// Inject logger into context
	logger, err := do.Invoke[*slog.Logger](injector)
	if err != nil || logger == nil {
		slox.Warn(ctx, "failed to retrieve logger, continuing with default logger", slog.Any("error", err))

		logger = slog.Default()
		do.Provide(injector, func(_ do.Injector) (*slog.Logger, error) {
			return logger, nil
		})
	}

	ctx = slox.Into(ctx, logger)
	shutdownCtx = slox.Into(shutdownCtx, logger)

	startPool := pool.New().
		WithErrors().
		WithContext(shutdownCtx).
		WithCancelOnError()

	// Start all modules
	for _, module := range r.modules {
		startPool.Go(func(ctx context.Context) error {
			name := fmt.Sprintf("%T", module)

			switch m := module.(type) {
			case AsyncModule:
				if err := m.StartAsync(ctx); err != nil {
					slox.Error(
						ctx,
						"failed starting async module",
						slog.String("name", name),
						slog.Any("error", err),
					)

					return oops.
						With("name", name).
						Wrapf(err, "failed starting module")
				}
			case SyncModule:
				if err := m.Start(ctx); err != nil {
					slox.Error(
						ctx,
						"failed starting sync module",
						slog.String("name", name),
						slog.Any("error", err),
					)

					return oops.
						With("name", name).
						Wrapf(err, "failed starting module")
				}
			default:
				slox.Debug(
					ctx,
					"skippig starting module without any start function",
					slog.String("name", name),
				)

				return nil
			}

			return nil
		})
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- startPool.Wait()
	}()

	select {
	case <-shutdownCtx.Done():
		slox.Info(ctx, "shutdown signal received")
	case err := <-startDone:
		if err != nil {
			slox.Error(ctx, "modules failed", slog.Any("error", err))
			return err
		}
	}

	stop()

	shutdownTimeout, cancel := context.WithTimeout(ctx, DefaultShutdownTimeout)
	defer cancel()

	shutdownPool := pool.New().
		WithErrors().
		WithContext(shutdownTimeout)

	for _, module := range r.modules {
		shutdownPool.Go(func(ctx context.Context) error {
			name := fmt.Sprintf("%T", module)

			if err := module.Shutdown(ctx); err != nil {
				slox.Error(
					ctx,
					"failed shutting down module",
					slog.String("name", name),
					slog.Any("error", err),
				)

				return oops.
					With("name", name).
					Wrapf(err, "failed shutting down module")
			}

			return nil
		})
	}

	if err := shutdownPool.Wait(); err != nil {
		slox.Error(ctx, "failed shutting down modules", slog.Any("error", err))
		return err //nolint:wrapcheck
	}

	return nil
}
