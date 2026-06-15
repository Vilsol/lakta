package pgx

import (
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/knadh/koanf/v2"
)

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level string
		want  tracelog.LogLevel
	}{
		{lvlTrace, tracelog.LogLevelTrace},
		{lvlDebug, tracelog.LogLevelDebug},
		{lvlInfo, tracelog.LogLevelInfo},
		{lvlWarn, tracelog.LogLevelWarn},
		{"warning", tracelog.LogLevelWarn},
		{lvlError, tracelog.LogLevelError},
		{"none", tracelog.LogLevelNone},
		{"WARN", tracelog.LogLevelWarn},  // case-insensitive
		{"", tracelog.LogLevelInfo},      // default
		{"bogus", tracelog.LogLevelInfo}, // unknown -> default
	}

	for _, c := range cases {
		t.Run(c.level, func(t *testing.T) {
			t.Parallel()
			cfg := Config{LogLevel: c.level}
			testza.AssertEqual(t, c.want, cfg.ParseLogLevel())
		})
	}
}

func TestNewPoolConfig_AppliesConnCounts(t *testing.T) {
	t.Parallel()

	c := NewConfig(
		WithDSN("postgres://u:p@localhost:5432/db"),
		WithMaxOpenConns(42),
		WithMinConns(7),
	)
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, int32(42), pc.MaxConns)
	testza.AssertEqual(t, int32(7), pc.MinConns)
}

func TestNewPoolConfig_InvalidDSN(t *testing.T) {
	t.Parallel()

	c := NewConfig(WithDSN("://not a valid dsn"))
	c.logLevelParsed = c.ParseLogLevel()

	pc, err := c.NewPoolConfig()
	testza.AssertNotNil(t, err)
	testza.AssertNil(t, pc)
}

func TestLoadFromKoanf_DecodesValues(t *testing.T) {
	t.Parallel()

	k := koanf.New(".")
	testza.AssertNil(t, k.Load(testkit.MapProvider(map[string]any{
		"modules": map[string]any{
			"db": map[string]any{
				"pgx": map[string]any{
					"default": map[string]any{ //nolint:gosec // test DSN, not a real credential
						"dsn":               "postgres://u:p@localhost:5432/db",
						"max_open_conns":    25,
						"min_conns":         3,
						"log_level":         "warn",
						"max_conn_lifetime": "2h",
					},
				},
			},
		},
	}), nil))

	m := NewModule()
	testza.AssertNil(t, m.LoadConfig(k))
	testza.AssertNil(t, m.Init(t.Context()))

	testza.AssertEqual(t, "postgres://u:p@localhost:5432/db", m.config.DSN)
	testza.AssertEqual(t, int32(25), m.config.MaxOpenConns)
	testza.AssertEqual(t, int32(3), m.config.MinConns)
	testza.AssertEqual(t, "warn", m.config.LogLevel)
	testza.AssertEqual(t, 2*time.Hour, m.config.MaxConnLifetime)
	testza.AssertEqual(t, tracelog.LogLevelWarn, m.config.GetLogLevel())
}
