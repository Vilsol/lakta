package lakta

import "github.com/knadh/koanf/v2"

// Configurable is implemented by modules that can load configuration from koanf.
type Configurable interface {
	// ConfigPath returns the koanf path for this module's configuration.
	// Example: "modules.grpc.server.default"
	ConfigPath() string

	// LoadConfig loads configuration from koanf into the module's config struct.
	LoadConfig(k *koanf.Koanf) error
}

// NamedModule is implemented by modules that support instance naming.
type NamedModule interface {
	// Name returns the instance name for this module.
	Name() string
}

// ReloadNotifier can register callbacks invoked after config hot-reload, and
// validators invoked before a reload is committed.
type ReloadNotifier interface {
	OnReload(fn func(k *koanf.Koanf))
	OnValidate(fn func(k *koanf.Koanf) error)
}

// HotReloadable is implemented by modules that react to config hot-reload.
// The runtime automatically registers them with the ReloadNotifier after all Init calls complete.
type HotReloadable interface {
	OnReload(k *koanf.Koanf)
}

// ValidatableModule can veto a config hot-reload before it is committed.
// A non-nil error aborts the reload; the previous config stays live.
// ValidateReload runs under the config module's reload lock and must not call
// back into the config module (e.g. Koanf()).
type ValidatableModule interface {
	ValidateReload(k *koanf.Koanf) error
}
