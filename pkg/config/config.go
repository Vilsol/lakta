// Package config provides a configuration system for Lakta using koanf.
// It supports loading configuration from YAML, JSON, and TOML files,
// environment variables, and CLI flags with hot-reload support.
package config

import (
	"time"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultEnvPrefix     = "LAKTA_"
	defaultConfigName    = "lakta"
	defaultDebounceDelay = 100 * time.Millisecond
)

// ReloadNotifier is an alias for lakta.ReloadNotifier.
type ReloadNotifier = lakta.ReloadNotifier

// Config holds the configuration for the config module.
type Config struct {
	// EnvPrefix specifies the prefix for environment variables used to override configuration values.
	EnvPrefix string

	// ConfigDirs specifies the directories to search for configuration files in the given order.
	ConfigDirs []string

	// ConfigName specifies the base name of the configuration file without its file extension.
	ConfigName string

	// Args contains the command-line arguments to be parsed for configuration overrides.
	Args []string

	// DebounceDelay is how long to wait after a file change event before reloading config.
	// Defaults to 100ms. Set to a lower value in tests.
	DebounceDelay time.Duration
}

// Option manipulates Config.
type Option func(cfg *Config)

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		EnvPrefix:     defaultEnvPrefix,
		ConfigDirs:    []string{".", "./config", "/etc/lakta"},
		ConfigName:    defaultConfigName,
		Args:          nil,
		DebounceDelay: defaultDebounceDelay,
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return Apply(NewDefaultConfig(), options...)
}

// WithEnvPrefix sets the environment variable prefix (default: "LAKTA_").
func WithEnvPrefix(prefix string) Option {
	return func(cfg *Config) {
		cfg.EnvPrefix = prefix
	}
}

// WithConfigDirs sets directories to search for config files.
func WithConfigDirs(dirs ...string) Option {
	return func(cfg *Config) {
		cfg.ConfigDirs = dirs
	}
}

// WithConfigName sets the base config file name without extension (default: "lakta").
func WithConfigName(name string) Option {
	return func(cfg *Config) {
		cfg.ConfigName = name
	}
}

// WithDebounceDelay sets how long to wait after a file change before reloading (default: 100ms).
func WithDebounceDelay(d time.Duration) Option {
	return func(cfg *Config) {
		cfg.DebounceDelay = d
	}
}

// WithArgs sets CLI arguments to parse for config overrides.
func WithArgs(args []string) Option {
	return func(cfg *Config) {
		cfg.Args = args
	}
}

// Apply applies options to a copy of defaults and returns the result.
func Apply[C any, O ~func(*C)](defaults C, opts ...O) C { //nolint:ireturn
	for _, o := range opts {
		o(&defaults)
	}
	return defaults
}

// UnmarshalKoanf loads configuration from koanf at the given path into c.
func UnmarshalKoanf[C any](c *C, k *koanf.Koanf, path string) error {
	return oops.Wrapf(k.Unmarshal(path, c), "failed to load config from koanf at path %s", path)
}
