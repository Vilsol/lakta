package policy

import (
	"context"
	"maps"
	"slices"

	"github.com/failsafe-go/failsafe-go"
	"github.com/samber/oops"
)

// Registry holds the named policy executors defined in config and code.
type Registry struct {
	executors map[string]failsafe.Executor[any]
}

// Executor returns the named policy's executor, or an error listing the
// known policy names.
func (r *Registry) Executor(name string) (failsafe.Executor[any], error) {
	ex, ok := r.executors[name]
	if !ok {
		known := slices.Sorted(maps.Keys(r.executors))
		return nil, oops.Errorf("unknown resilience policy %q (known policies: %v)", name, known)
	}
	return ex, nil
}

// Run executes fn through the named policy chain. The context passed to fn
// carries policy-driven cancellation (e.g. timeouts).
func (r *Registry) Run(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	ex, err := r.Executor(name)
	if err != nil {
		return err
	}
	//nolint:wrapcheck // policy and handler errors pass through unwrapped so callers can match sentinels like circuitbreaker.ErrOpen
	return ex.WithContext(ctx).RunWithExecution(func(exec failsafe.Execution[any]) error {
		return fn(exec.Context())
	})
}

// Get executes fn through the named policy chain and returns its typed result.
//
//nolint:ireturn // generic accessor returns the execution result
func Get[T any](ctx context.Context, r *Registry, name string, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	ex, err := r.Executor(name)
	if err != nil {
		return zero, err
	}

	res, err := ex.WithContext(ctx).GetWithExecution(func(exec failsafe.Execution[any]) (any, error) {
		return fn(exec.Context())
	})
	if err != nil {
		return zero, err //nolint:wrapcheck // policy and handler errors pass through unwrapped so callers can match sentinels
	}
	// Policies are type-erased to any; the wrapper above always produces T.
	v, ok := res.(T)
	if !ok {
		return zero, oops.Errorf("resilience policy %q returned unexpected type %T", name, res)
	}
	return v, nil
}
