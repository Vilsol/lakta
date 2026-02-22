package health

import "github.com/Vilsol/lakta/pkg/lakta"

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Provider     = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)
