package lakta

import (
	"context"

	"github.com/samber/do/v2"
)

type injectorKey struct{}

// GetInjector returns the injector from the context.
func GetInjector(ctx context.Context) do.Injector { //nolint:ireturn
	injector, ok := ctx.Value(injectorKey{}).(do.Injector)
	if !ok {
		panic("injector not found in context")
	}
	return injector
}

// WithInjector returns a new context with the injector set.
func WithInjector(ctx context.Context, injector do.Injector) context.Context {
	return context.WithValue(ctx, injectorKey{}, injector)
}

// Provide injects a provider into the injector.
func Provide[T any](ctx context.Context, provider do.Provider[T]) {
	do.Provide(GetInjector(ctx), provider)
}
