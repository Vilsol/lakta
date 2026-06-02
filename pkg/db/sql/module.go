package sql

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/Masterminds/squirrel"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

// Module provides a squirrel query builder via DI.
type Module struct{}

// NewModule creates a new SQL query builder module.
func NewModule() *Module {
	return &Module{}
}

// Init registers the query builder provider in the DI container.
func (m *Module) Init(ctx context.Context) error {
	lakta.Provide(ctx, m.GetInstance)
	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*squirrel.StatementBuilderType](),
	}
}

// Dependencies declares the required types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return []reflect.Type{
		reflect.TypeFor[*sql.DB](),
	}, nil
}

// Shutdown is a no-op for this module.
func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

// GetInstance returns a configured squirrel statement builder backed by the injected *sql.DB.
func (m *Module) GetInstance(injector do.Injector) (*squirrel.StatementBuilderType, error) {
	db, err := do.Invoke[*sql.DB](injector)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to retrieve SQL runner")
	}

	cache := squirrel.NewStmtCache(db)
	instance := squirrel.StatementBuilder.
		PlaceholderFormat(squirrel.Dollar).
		RunWith(cache)

	return &instance, nil
}
