package tint

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/lmittmann/tint"
)

// Config represents configuration for Tint [Module]
type Config struct {
	// Instance name (determines config path, cannot come from config file)
	Name string `koanf:"-"`

	// Code-only fields
	Writer io.Writer `koanf:"-"`

	// File-configurable fields
	Level      string `koanf:"level"`
	TimeFormat string `koanf:"time_format"`

	levelParsed slog.Level
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:        config.DefaultInstanceName,
		Writer:      os.Stderr,
		Level:       "debug",
		TimeFormat:  time.RFC3339,
		levelParsed: slog.LevelDebug,
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
	return k.Unmarshal(path, c)
}

// ParseLevel parses the string level into slog.Level.
func (c *Config) ParseLevel() slog.Level {
	switch strings.ToLower(c.Level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

// GetLevel returns the parsed slog.Level.
func (c *Config) GetLevel() slog.Level {
	return c.levelParsed
}

// TintOptions returns tint.Options with config values applied.
func (c *Config) TintOptions() *tint.Options {
	return &tint.Options{
		AddSource:  true,
		Level:      c.levelParsed,
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

// WithWriter sets the output writer (code-only, cannot be configured via files).
func WithWriter(writer io.Writer) Option {
	return func(m *Config) { m.Writer = writer }
}
