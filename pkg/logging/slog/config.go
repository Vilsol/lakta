package slog

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// Log level string representations.
// Built-in output formats.
const (
	FormatJSON = "json"
	FormatText = "text"
)

const (
	levelDebug = "debug"
	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"
)

// Config represents configuration for slog [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Level represents the default log level to be used in the configuration.
	Level string `koanf:"level"`

	// Levels defines a map of per-package log level overrides.
	Levels map[string]string `koanf:"levels"`

	// GlobalDefault indicates whether the logger should be set as the default globally.
	GlobalDefault bool `koanf:"global_default"`

	// Format selects a built-in output handler: "json" (stdlib JSON with keys
	// time, level, message) or "text" (tint, for local development). Empty uses
	// the slog.Handler another module provides in DI.
	Format string `enum:"json,text" koanf:"format"`

	// Service is added to every line as "service" when Format is set.
	Service string `koanf:"service"`

	// Version is added to every line as "version" when Format is set.
	Version string `koanf:"version"`

	// Writer receives built-in handler output. Defaults to os.Stdout.
	Writer io.Writer `code_only:"WithWriter" koanf:"-"`

	// levelParsed stores the parsed log level from the Level field.
	levelParsed slog.Level

	// levelsParsed holds the parsed per-package log levels.
	levelsParsed map[string]slog.Level
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:          config.DefaultInstanceName,
		Level:         levelInfo,
		GlobalDefault: true,
		Writer:        os.Stdout,
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

// ParseLevel parses the string level into slog.Level.
func (c *Config) ParseLevel() {
	c.levelParsed = parseLevel(c.Level)
}

// ParseLevels parses all per-package level strings into slog.Levels.
func (c *Config) ParseLevels() {
	if len(c.Levels) == 0 {
		return
	}

	c.levelsParsed = make(map[string]slog.Level, len(c.Levels))
	for pkg, lvl := range c.Levels {
		c.levelsParsed[pkg] = parseLevel(lvl)
	}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case levelDebug:
		return slog.LevelDebug
	case levelInfo:
		return slog.LevelInfo
	case levelWarn, "warning":
		return slog.LevelWarn
	case levelError:
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithLevel sets the default log level.
func WithLevel(level string) Option {
	return func(m *Config) { m.Level = level }
}

// WithLevels sets per-package log level overrides.
func WithLevels(levels map[string]string) Option {
	return func(m *Config) { m.Levels = levels }
}

// WithFormat selects a built-in output handler ("json" or "text").
func WithFormat(format string) Option {
	return func(m *Config) { m.Format = format }
}

// WithService sets the "service" attribute of built-in handlers.
func WithService(service string) Option {
	return func(m *Config) { m.Service = service }
}

// WithVersion sets the "version" attribute of built-in handlers.
func WithVersion(version string) Option {
	return func(m *Config) { m.Version = version }
}

// WithWriter sets where built-in handlers write.
func WithWriter(w io.Writer) Option {
	return func(m *Config) { m.Writer = w }
}
