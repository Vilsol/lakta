package pgx

import "testing"

// FuzzNewPoolConfig ensures arbitrary DSN strings never panic — they must
// resolve to either a pool config or a wrapped parse error.
func FuzzNewPoolConfig(f *testing.F) {
	for _, seed := range []string{
		"postgres://user:pass@localhost:5432/db",
		"host=localhost port=5432 user=test",
		"",
		"not a dsn",
		"postgres://",
		"://bad",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, dsn string) {
		c := &Config{DSN: dsn}
		_, _ = c.NewPoolConfig()
	})
}
