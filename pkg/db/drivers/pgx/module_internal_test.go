package pgx

import "github.com/Vilsol/lakta/pkg/lakta"

var (
	_ lakta.AsyncModule  = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
)
