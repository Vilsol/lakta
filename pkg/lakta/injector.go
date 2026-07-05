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

// tryInjector is the non-panicking sibling of GetInjector: it reports whether
// ctx carries an injector instead of panicking, so RunContext can adopt a
// pre-seeded one. Unexported — GetInjector keeps its panic contract.
func tryInjector(ctx context.Context) (do.Injector, bool) { //nolint:ireturn
	inj, ok := ctx.Value(injectorKey{}).(do.Injector)
	return inj, ok
}

// WithInjector returns a new context with the injector set.
func WithInjector(ctx context.Context, injector do.Injector) context.Context {
	return context.WithValue(ctx, injectorKey{}, injector)
}

// HasInjector reports whether ctx carries a DI injector. Modules whose Init may
// run under a bare (test) context use it to guard optional DI access that would
// otherwise panic via GetInjector.
func HasInjector(ctx context.Context) bool {
	_, ok := tryInjector(ctx)
	return ok
}

// Provide injects a provider into the injector.
func Provide[T any](ctx context.Context, provider do.Provider[T]) {
	do.Provide(GetInjector(ctx), provider)
}

// ProvideValue injects a pre-created value into the injector.
func ProvideValue[T any](ctx context.Context, value T) {
	do.Provide(GetInjector(ctx), func(_ do.Injector) (T, error) {
		return value, nil
	})
}

// Invoke retrieves a value of type T from the injector in the context.
func Invoke[T any](ctx context.Context) (T, error) { //nolint:ireturn
	return do.Invoke[T](GetInjector(ctx))
}
