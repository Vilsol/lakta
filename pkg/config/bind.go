package config

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

// Validatable is implemented by config structs that need validation after unmarshalling.
type Validatable interface {
	Validate() error
}

// Binding is a thread-safe, cached config accessor with hot-reload support.
type Binding[T any] struct {
	cached   atomic.Pointer[T]
	mu       sync.Mutex
	onChange []func(*T)
}

// Get returns the cached config value (zero-alloc atomic pointer load).
func (b *Binding[T]) Get() *T {
	return b.cached.Load()
}

// OnChange registers a callback invoked with the new config value after each reload.
func (b *Binding[T]) OnChange(fn func(*T)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChange = append(b.onChange, fn)
}

func (b *Binding[T]) update(cfg *T) {
	b.cached.Store(cfg)

	b.mu.Lock()
	callbacks := make([]func(*T), len(b.onChange))
	copy(callbacks, b.onChange)
	b.mu.Unlock()

	for _, fn := range callbacks {
		fn(cfg)
	}
}

type bindModule[T any] struct {
	path    string
	binding *Binding[T]
}

// Bind creates a module that binds a config struct to a koanf path and registers
// it in DI as *Binding[T]. Path segments are joined with "." (e.g. "app", "limits" → "app.limits").
func Bind[T any](pathSegments ...string) *bindModule[T] {
	return &bindModule[T]{
		path: strings.Join(pathSegments, "."),
	}
}

func unmarshalAndValidate[T any](k *koanf.Koanf, path string) (*T, error) {
	cfg := new(T)
	if err := k.Unmarshal(path, cfg); err != nil {
		return nil, oops.Wrapf(err, "failed to unmarshal config at path %q", path)
	}

	if v, ok := any(cfg).(Validatable); ok {
		if err := v.Validate(); err != nil {
			return nil, oops.Wrapf(err, "config validation failed at path %q", path)
		}
	}

	return cfg, nil
}

func (m *bindModule[T]) Init(ctx context.Context) error {
	injector := lakta.GetInjector(ctx)

	k, err := do.Invoke[*koanf.Koanf](injector)
	if err != nil {
		return oops.Wrapf(err, "failed to retrieve koanf instance")
	}

	cfg, err := unmarshalAndValidate[T](k, m.path)
	if err != nil {
		return err
	}

	m.binding = &Binding[T]{}
	m.binding.cached.Store(cfg)

	lakta.Provide(ctx, func(_ do.Injector) (*Binding[T], error) {
		return m.binding, nil
	})

	if notifier, err := do.Invoke[ReloadNotifier](injector); err == nil {
		notifier.OnReload(func(k *koanf.Koanf) {
			newCfg, err := unmarshalAndValidate[T](k, m.path)
			if err != nil {
				return
			}
			m.binding.update(newCfg)
		})
	}

	return nil
}

func (m *bindModule[T]) Shutdown(_ context.Context) error {
	return nil
}

func (m *bindModule[T]) ConfigPath() string {
	return m.path
}

func (m *bindModule[T]) LoadConfig(k *koanf.Koanf) error {
	cfg, err := unmarshalAndValidate[T](k, m.path)
	if err != nil {
		return err
	}

	m.binding.update(cfg)

	return nil
}

// Get returns the cached config value from DI. Zero-alloc hot path.
func Get[T any](ctx context.Context) *T {
	return do.MustInvoke[*Binding[T]](lakta.GetInjector(ctx)).Get()
}

// GetBinding returns the Binding for advanced use (OnChange callbacks).
func GetBinding[T any](ctx context.Context) *Binding[T] {
	return do.MustInvoke[*Binding[T]](lakta.GetInjector(ctx))
}
