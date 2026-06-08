package pgx

import (
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
)

var (
	_ lakta.AsyncModule  = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)

func TestNewPoolConfig_AppliesPoolDefaults(t *testing.T) {
	t.Parallel()

	c := NewConfig(WithDSN("postgres://u:p@localhost:5432/db"))
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNoError(t, err)

	testza.AssertEqual(t, time.Hour, pc.MaxConnLifetime)
	testza.AssertEqual(t, 30*time.Minute, pc.MaxConnIdleTime)
	testza.AssertEqual(t, time.Minute, pc.HealthCheckPeriod)
	testza.AssertEqual(t, "30000", pc.ConnConfig.RuntimeParams["statement_timeout"])
}

func TestNewPoolConfig_StatementTimeoutDisabledWhenZero(t *testing.T) {
	t.Parallel()

	c := NewConfig(WithDSN("postgres://u:p@localhost:5432/db"), WithStatementTimeout(0))
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNoError(t, err)

	_, ok := pc.ConnConfig.RuntimeParams["statement_timeout"]
	testza.AssertFalse(t, ok)
}

func TestNewPoolConfig_StatementTimeoutCustomValue(t *testing.T) {
	t.Parallel()

	c := NewConfig(WithDSN("postgres://u:p@localhost:5432/db"), WithStatementTimeout(5*time.Second))
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "5000", pc.ConnConfig.RuntimeParams["statement_timeout"])
}
