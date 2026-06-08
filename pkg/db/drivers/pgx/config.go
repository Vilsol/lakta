package pgx

import (
	"strconv"
	"strings"
	"time"

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

const (
	defaultMaxConnLifetime   = time.Hour
	defaultMaxConnIdleTime   = 30 * time.Minute
	defaultHealthCheckPeriod = time.Minute
	defaultStatementTimeout  = 30 * time.Second
)

// Config represents configuration for SQL Databse [Module].
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

	// MinConns is the minimum number of idle connections kept in the pool.
	MinConns int32 `koanf:"min_conns"`

	// MaxConnLifetime is the maximum age of a connection before it is closed.
	MaxConnLifetime time.Duration `koanf:"max_conn_lifetime"`

	// MaxConnIdleTime is the maximum idle time before a connection is closed.
	MaxConnIdleTime time.Duration `koanf:"max_conn_idle_time"`

	// HealthCheckPeriod is how often the pool checks idle connection health.
	HealthCheckPeriod time.Duration `koanf:"health_check_period"`

	// StatementTimeout sets the per-statement timeout (Postgres statement_timeout). Zero disables it.
	StatementTimeout time.Duration `koanf:"statement_timeout"`

	// logLevelParsed stores the parsed representation of the LogLevel field.
	logLevelParsed tracelog.LogLevel `koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name:              config.DefaultInstanceName,
		DSN:               "",
		MaxOpenConns:      defaultMaxOpenConns,
		LogLevel:          "info",
		logLevelParsed:    tracelog.LogLevelInfo,
		MinConns:          0,
		MaxConnLifetime:   defaultMaxConnLifetime,
		MaxConnIdleTime:   defaultMaxConnIdleTime,
		HealthCheckPeriod: defaultHealthCheckPeriod,
		StatementTimeout:  defaultStatementTimeout,
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return config.Apply(NewDefaultConfig(), options...)
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to load config from koanf at path %s", path)
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
	poolConfig.MinConns = c.MinConns
	poolConfig.MaxConnLifetime = c.MaxConnLifetime
	poolConfig.MaxConnIdleTime = c.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = c.HealthCheckPeriod

	if c.StatementTimeout > 0 {
		if poolConfig.ConnConfig.RuntimeParams == nil {
			poolConfig.ConnConfig.RuntimeParams = map[string]string{}
		}
		poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(c.StatementTimeout.Milliseconds(), 10)
	}

	poolConfig.ConnConfig.Tracer = multitracer.New(
		&tracelog.TraceLog{
			Logger:   newLogger(),
			LogLevel: c.logLevelParsed,
			Config:   tracelog.DefaultTraceLogConfig(),
		},
		otelpgx.NewTracer(),
		newQueryMetricsTracer(poolConfig.ConnConfig.Database),
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

// WithMinConns sets the minimum number of idle pool connections.
func WithMinConns(n int32) Option {
	return func(m *Config) { m.MinConns = n }
}

// WithMaxConnLifetime sets the maximum connection age.
func WithMaxConnLifetime(d time.Duration) Option {
	return func(m *Config) { m.MaxConnLifetime = d }
}

// WithMaxConnIdleTime sets the maximum connection idle time.
func WithMaxConnIdleTime(d time.Duration) Option {
	return func(m *Config) { m.MaxConnIdleTime = d }
}

// WithHealthCheckPeriod sets the pool health-check interval.
func WithHealthCheckPeriod(d time.Duration) Option {
	return func(m *Config) { m.HealthCheckPeriod = d }
}

// WithStatementTimeout sets the Postgres statement_timeout. Zero disables it.
func WithStatementTimeout(d time.Duration) Option {
	return func(m *Config) { m.StatementTimeout = d }
}
