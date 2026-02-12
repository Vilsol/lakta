package pgx

import (
	"strings"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultMaxOpenConns = 10
)

// Config represents configuration for SQL Databse [Module]
type Config struct {
	// Instance name (determines config path, cannot come from config file)
	Name string `koanf:"-"`

	// File-configurable fields
	DSN          string `koanf:"dsn"`
	MaxOpenConns int32  `koanf:"max_open_conns"`
	LogLevel     string `koanf:"log_level"`
	HealthCheck  bool   `koanf:"health_check"`

	logLevelParsed tracelog.LogLevel
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:           config.DefaultInstanceName,
		DSN:            "",
		MaxOpenConns:   defaultMaxOpenConns,
		LogLevel:       "info",
		logLevelParsed: tracelog.LogLevelInfo,
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

// ParseLogLevel parses the string log level into tracelog.LogLevel.
func (c *Config) ParseLogLevel() tracelog.LogLevel {
	switch strings.ToLower(c.LogLevel) {
	case "trace":
		return tracelog.LogLevelTrace
	case "debug":
		return tracelog.LogLevelDebug
	case "info":
		return tracelog.LogLevelInfo
	case "warn", "warning":
		return tracelog.LogLevelWarn
	case "error":
		return tracelog.LogLevelError
	case "none":
		return tracelog.LogLevelNone
	default:
		return tracelog.LogLevelInfo
	}
}

// GetLogLevel returns the parsed tracelog.LogLevel.
func (c *Config) GetLogLevel() tracelog.LogLevel {
	return c.logLevelParsed
}

// NewPoolConfig parses the DSN and configures the pool with settings from config.
func (c *Config) NewPoolConfig() (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(c.DSN)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to parse database DSN")
	}

	poolConfig.MaxConns = c.MaxOpenConns
	poolConfig.ConnConfig.Tracer = multitracer.New(
		&tracelog.TraceLog{
			Logger:   newLogger(),
			LogLevel: c.logLevelParsed,
			Config:   tracelog.DefaultTraceLogConfig(),
		},
		otelpgx.NewTracer(),
	)

	return poolConfig, nil
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}
