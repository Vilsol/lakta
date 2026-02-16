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
	// Instance name
	Name string `koanf:"-"`

	// DSN is the database connection string used to configure the database connection.
	DSN string `koanf:"dsn" required:"true"`

	// MaxOpenConns specifies the maximum number of open connections to the database. It maps to the "max_open_conns" configuration.
	MaxOpenConns int32 `koanf:"max_open_conns"`

	// LogLevel specifies the logging level for database operations, supporting values like trace, debug, info, warn, error, none.
	LogLevel string `enum:"trace,debug,info,warn,error,none" koanf:"log_level"`

	// HealthCheck enables or disables the database health check mechanism.
	HealthCheck bool `koanf:"health_check"`

	// logLevelParsed stores the parsed representation of the LogLevel field.
	logLevelParsed tracelog.LogLevel `koanf:"-"`
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
	return oops.Wrapf(k.Unmarshal(path, c), "failed to load config from koanf at path %s", path)
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

// WithDSN sets the database connection string.
func WithDSN(dsn string) Option {
	return func(m *Config) { m.DSN = dsn }
}

// WithMaxOpenConns sets the maximum number of open connections.
func WithMaxOpenConns(n int32) Option {
	return func(m *Config) { m.MaxOpenConns = n }
}

// WithLogLevel sets the database log level.
func WithLogLevel(level string) Option {
	return func(m *Config) { m.LogLevel = level }
}

// WithHealthCheck enables or disables the database health check.
func WithHealthCheck(enabled bool) Option {
	return func(m *Config) { m.HealthCheck = enabled }
}
