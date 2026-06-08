// Package pgx provides a lakta module for PostgreSQL connections using pgx.
//
// Default pool settings are applied: MaxConnLifetime 1h, MaxConnIdleTime 30m,
// HealthCheckPeriod 1m, and a Postgres statement_timeout of 30s. The
// statement_timeout can be raised or disabled (set to 0) via WithStatementTimeout
// or the koanf config key — important for long migrations or reports. MinConns
// and connection lifetimes are configurable via the respective With* options or
// koanf keys.
package pgx
