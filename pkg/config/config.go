// Package config provides a configuration system for Lakta using koanf.
// It supports loading configuration from YAML, JSON, and TOML files,
// environment variables, and CLI flags with hot-reload support.
package config

import "github.com/knadh/koanf/v2"

const (
	defaultEnvPrefix  = "LAKTA_"
	defaultConfigName = "lakta"
)

// ReloadNotifier can register callbacks for config reload events.
type ReloadNotifier interface {
	OnReload(fn func(k *koanf.Koanf))
}

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
}

// Option manipulates Config.
type Option func(cfg *Config)

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		EnvPrefix:  defaultEnvPrefix,
		ConfigDirs: []string{".", "./config", "/etc/lakta"},
		ConfigName: defaultConfigName,
		Args:       nil,
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

// WithArgs sets CLI arguments to parse for config overrides.
func WithArgs(args []string) Option {
	return func(cfg *Config) {
		cfg.Args = args
	}
}
