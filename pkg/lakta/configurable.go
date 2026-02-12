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
