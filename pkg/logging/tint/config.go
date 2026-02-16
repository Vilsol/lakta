package tint

import (
	"io"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/lmittmann/tint"
	"github.com/samber/oops"
)

// Config represents configuration for Tint [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Writer specifies the output destination for logs.
	Writer io.Writer `code_only:"WithWriter" koanf:"-"`

	// TimeFormat specifies the format for timestamping log entries.
	TimeFormat string `koanf:"time_format"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:       config.DefaultInstanceName,
		Writer:     os.Stderr,
		TimeFormat: time.RFC3339,
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	cfg := NewDefaultConfig()
	for _, option := range options {
		option(&cfg)
	}
	return cfg
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(k.Unmarshal(path, c), "failed to load config from koanf at path %s", path)
}

// TintOptions returns tint.Options with config values applied.
// Level is set to accept all messages; filtering is handled by the slog module's levelFilter.
func (c *Config) TintOptions() *tint.Options {
	return &tint.Options{
		AddSource:  true,
		Level:      slog.Level(math.MinInt),
		TimeFormat: c.TimeFormat,
	}
}

// NewHandler creates a new tint handler with config values applied.
func (c *Config) NewHandler() slog.Handler {
	return tint.NewHandler(c.Writer, c.TintOptions())
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithTimeFormat sets the time format string.
func WithTimeFormat(format string) Option {
	return func(m *Config) { m.TimeFormat = format }
}

// WithWriter sets the output writer (code-only, cannot be configured via files).
func WithWriter(writer io.Writer) Option {
	return func(m *Config) { m.Writer = writer }
}
