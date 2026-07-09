package memory

import (
	"maps"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Spec declares one named cache's sizing. v1 keys only — refresh_after and
// max_weight are intentionally trimmed.
type Spec struct {
	MaxSize     int           `koanf:"max_size"`     // entry-count bound -> otter MaximumSize
	TTL         time.Duration `koanf:"ttl"`          // expire-after-write; 0 = none
	TTLAccess   time.Duration `koanf:"ttl_access"`   // expire-after-access; 0 = none
	RecordStats bool          `koanf:"record_stats"` // attach the otel StatsRecorder
}

// Config mirrors pool.Config: config Caches overlay code-only CodeCaches by name.
type Config struct {
	// Instance name.
	Name string `koanf:"-"`

	// Caches holds config-declared caches. Prefer snake_case names; hyphens
	// cannot be overridden via environment variables.
	Caches map[string]Spec `koanf:"caches"`

	// CodeCaches holds caches registered via WithCache (code-only); a config
	// entry with the same name replaces it wholesale.
	CodeCaches map[string]Spec `code_only:"WithCache" koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name: config.DefaultInstanceName,
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return config.Apply(NewDefaultConfig(), options...)
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// MergedCaches combines code-registered and config-defined caches; config wins
// per name.
func (c *Config) MergedCaches() map[string]Spec {
	merged := make(map[string]Spec, len(c.CodeCaches)+len(c.Caches))
	maps.Copy(merged, c.CodeCaches)
	maps.Copy(merged, c.Caches)
	return merged
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithCache registers a cache in code (code-only); config with the same name
// takes precedence.
func WithCache(name string, spec Spec) Option {
	return func(m *Config) {
		if m.CodeCaches == nil {
			m.CodeCaches = make(map[string]Spec)
		}
		m.CodeCaches[name] = spec
	}
}
