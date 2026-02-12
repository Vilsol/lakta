// Package config provides a configuration system for Lakta using koanf.
// It supports loading configuration from YAML, JSON, and TOML files,
// environment variables, and CLI flags with hot-reload support.
package config

const (
	defaultEnvPrefix  = "LAKTA_"
	defaultConfigName = "lakta"
)

// Config holds the configuration for the config module.
type Config struct {
	EnvPrefix  string
	ConfigDirs []string
	ConfigName string
	Args       []string
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
