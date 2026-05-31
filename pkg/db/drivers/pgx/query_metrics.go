package pgx

import "strings"

const unnamedQuery = "unnamed"

// queryName extracts the sqlc query name from a leading "-- name: X :cmd"
// comment. Falls back to the leading SQL verb (uppercased), then "unnamed".
// It never returns raw SQL — the result is a bounded-cardinality metric label.
func queryName(sql string) string {
	for _, line := range strings.Split(sql, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "-- name:"); ok {
			if fields := strings.Fields(rest); len(fields) >= 1 {
				return fields[0]
			}
			return unnamedQuery
		}
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "/*") {
			continue // skip non-sqlc comments
		}
		if fields := strings.Fields(line); len(fields) >= 1 {
			return strings.ToUpper(fields[0])
		}
	}
	return unnamedQuery
}
