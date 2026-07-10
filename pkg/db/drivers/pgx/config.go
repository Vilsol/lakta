package pgx

import (
	"database/sql"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/knadh/koanf/v2"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/samber/oops"
)

const (
	defaultMaxOpenConns = 10
)

const (
	// lockAdvisory selects goose's Postgres advisory session locker, making
	// concurrent on-start runs across replicas serialize on a single lock so
	// each migration applies exactly once.
	lockAdvisory = "advisory"

	// defaultMigrationsTable is the history table goose records applied
	// versions in (overriding goose's own "goose_db_version" default).
	defaultMigrationsTable = "schema_migrations"

	// defaultMigrationsDir is the sub-path within the embedded FS holding the
	// .sql migration files (matches the //go:embed migrations/*.sql idiom).
	defaultMigrationsDir = "migrations"
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

	// Migrations configures goose-driven schema migrations for this instance.
	Migrations MigrationsConfig `koanf:"migrations"`

	// logLevelParsed stores the parsed representation of the LogLevel field.
	logLevelParsed tracelog.LogLevel `koanf:"-"`

	// migrationsFS is the embedded migration set; set via WithMigrations. It is
	// code-only (an fs.FS cannot come from YAML), so it never reaches koanf.
	migrationsFS fs.FS `code_only:"WithMigrations" koanf:"-"`
}

// MigrationsConfig configures goose-driven schema migrations for this instance.
//
// RunOnStart defaults to false: production applies migrations out-of-band from
// an init-container/job via RunMigrations, keeping on-start migration a dev
// convenience. See RunMigrations for the non-transactional wedge risk.
type MigrationsConfig struct {
	// RunOnStart applies pending migrations during StartAsync. Default false —
	// the init-container/out-of-band path is the prod-safe path.
	RunOnStart bool `koanf:"run_on_start"`

	// Table is the migration history table name.
	Table string `koanf:"table"`

	// Dir is the sub-path within the embedded FS that holds the .sql files.
	Dir string `koanf:"dir"`

	// Lock selects the on-start locking strategy: "advisory" uses a Postgres
	// session advisory lock (replica-safe), "none" disables locking.
	Lock string `enum:"advisory,none" koanf:"lock"`

	// AllowMissing applies out-of-order (missing) migrations instead of erroring.
	AllowMissing bool `koanf:"allow_missing"`
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
		Migrations: MigrationsConfig{
			RunOnStart:   false,
			Table:        defaultMigrationsTable,
			Dir:          defaultMigrationsDir,
			Lock:         lockAdvisory,
			AllowMissing: false,
		},
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
// safeParseConfig wraps pgxpool.ParseConfig, converting a panic on malformed
// input (e.g. the DSN `="\`) into an error so a bad config never crashes.
func safeParseConfig(dsn string) (*pgxpool.Config, error) {
	var (
		cfg *pgxpool.Config
		err error
	)

	func() {
		defer func() {
			if r := recover(); r != nil {
				err = oops.Errorf("panic parsing DSN: %v", r)
			}
		}()
		cfg, err = pgxpool.ParseConfig(dsn)
	}()

	if err != nil {
		return nil, oops.Wrapf(err, "failed to parse database DSN")
	}
	return cfg, nil
}

func (c *Config) NewPoolConfig() (*pgxpool.Config, error) {
	poolConfig, err := safeParseConfig(c.DSN)
	if err != nil {
		return nil, err
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

// WithMigrations sets the embedded migration filesystem (code-only; not from
// YAML). The FS root is expected to contain the Dir sub-path (default
// "migrations"), matching the //go:embed migrations/*.sql idiom.
func WithMigrations(fsys fs.FS) Option {
	return func(m *Config) { m.migrationsFS = fsys }
}

// WithMigrationsRunOnStart overrides Migrations.RunOnStart in code.
func WithMigrationsRunOnStart(v bool) Option {
	return func(m *Config) { m.Migrations.RunOnStart = v }
}

// GooseProvider builds a goose provider over db + fsys with the configured
// table, advisory session locker, and out-of-order toggle. It is the single
// migration code path shared by StartAsync and RunMigrations. Returns
// (nil, nil) when fsys is nil (no WithMigrations) so callers skip cleanly.
func (c *Config) GooseProvider(db *sql.DB, fsys fs.FS) (*goose.Provider, error) {
	if fsys == nil {
		return nil, nil
	}

	if c.Migrations.Dir != "" && c.Migrations.Dir != "." {
		sub, err := fs.Sub(fsys, c.Migrations.Dir)
		if err != nil {
			return nil, oops.Wrapf(err, "failed to open migrations dir %q in embedded FS", c.Migrations.Dir)
		}
		fsys = sub
	}

	var opts []goose.ProviderOption

	if c.Migrations.Lock == lockAdvisory {
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return nil, oops.Wrapf(err, "failed to create postgres session locker")
		}
		opts = append(opts, goose.WithSessionLocker(locker))
	}

	if c.Migrations.Table != "" {
		opts = append(opts, goose.WithTableName(c.Migrations.Table))
	}

	if c.Migrations.AllowMissing {
		opts = append(opts, goose.WithAllowOutofOrder(true))
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, opts...)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to build goose provider")
	}

	return provider, nil
}
