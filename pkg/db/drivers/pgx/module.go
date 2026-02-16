package pgx

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/hellofresh/health-go/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

var (
	_ lakta.AsyncModule  = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

// Module manages pgx connection pool lifecycle.
type Module struct {
	config      Config
	poolConfig  *pgxpool.Config
	instance    *pgxpool.Pool
	stdInstance *sql.DB
}

// NewModule creates a new pgx database module with the given options.
func NewModule(options ...Option) *Module {
	return &Module{config: NewConfig(options...)}
}

// Name returns the instance name.
func (m *Module) Name() string {
	return m.config.Name
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryDB, "pgx", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	path := m.ConfigPath()
	if k.Exists(path) {
		return m.config.LoadFromKoanf(k, path)
	}
	return nil
}

// Init loads configuration and prepares the connection pool config.
func (m *Module) Init(ctx context.Context) error {
	// Load config from koanf if available
	if k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx)); err == nil {
		if err := m.LoadConfig(k); err != nil {
			return oops.Wrapf(err, "failed to load config")
		}
	}

	// Parse the log level string
	m.config.logLevelParsed = m.config.ParseLogLevel()

	poolConfig, err := m.config.NewPoolConfig()
	if err != nil {
		return err
	}
	m.poolConfig = poolConfig

	return nil
}

// StartAsync connects to the database and registers the pool in the DI container.
func (m *Module) StartAsync(ctx context.Context) error {
	conn, err := pgxpool.NewWithConfig(ctx, m.poolConfig)
	if err != nil {
		return oops.With("dsn", m.config.DSN).
			Wrapf(err, "failed to connect to database")
	}

	var version string
	if err := conn.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return oops.Wrapf(err, "failed to query database version")
	}

	slox.Info(
		ctx,
		"connected to postgres database via pgx",
		slog.String("version", version),
		slog.String("host", m.poolConfig.ConnConfig.Host),
		slog.String("database", m.poolConfig.ConnConfig.Database),
	)

	m.instance = conn
	m.stdInstance = stdlib.OpenDBFromPool(m.instance)

	lakta.Provide(ctx, m.getConnection)
	lakta.Provide(ctx, m.getStdConnection)

	if m.config.HealthCheck {
		h, err := do.Invoke[*health.Health](lakta.GetInjector(ctx))
		if err != nil {
			return oops.Wrapf(err, "failed to get health instance")
		}
		if err := h.Register(health.Config{
			Name:    "postgres",
			Timeout: 2 * time.Second, //nolint:mnd
			Check: func(ctx context.Context) error {
				return m.instance.Ping(ctx)
			},
		}); err != nil {
			return oops.Wrapf(err, "failed to register postgres health check")
		}
	}

	return nil
}

// Shutdown closes the database connection.
func (m *Module) Shutdown(_ context.Context) error {
	if m.instance == nil {
		return nil
	}

	m.instance.Close()
	return nil
}

func (m *Module) getConnection(_ do.Injector) (*pgxpool.Pool, error) {
	return m.instance, nil
}

func (m *Module) getStdConnection(_ do.Injector) (*sql.DB, error) {
	return m.stdInstance, nil
}
