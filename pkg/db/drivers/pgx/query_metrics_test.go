package pgx

import (
	"testing"

	"github.com/MarvinJWendt/testza"
)

func TestQueryName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"sqlc many", "-- name: GetUsers :many\nSELECT * FROM users", "GetUsers"},
		{"sqlc one", "-- name: GetUserByID :one\nSELECT 1 WHERE id = $1", "GetUserByID"},
		{"no comment", "SELECT version()", "SELECT"},
		{"leading blank lines", "\n\nINSERT INTO t VALUES (1)", "INSERT"},
		{"other comment then sql", "-- license header\nUPDATE t SET x = 1", "UPDATE"},
		{"empty", "", "unnamed"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testza.AssertEqual(t, c.want, queryName(c.sql))
		})
	}
}
