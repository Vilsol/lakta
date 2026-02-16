package slog

import (
	"log/slog"
	"strings"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
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

	// levelParsed stores the parsed log level from the Level field.
	levelParsed slog.Level

	// levelsParsed holds the parsed per-package log levels.
	levelsParsed map[string]slog.Level
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:          config.DefaultInstanceName,
		Level:         "info",
		GlobalDefault: true,
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
