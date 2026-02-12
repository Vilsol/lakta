package sql

import (
	"context"
	"database/sql"

	"github.com/Masterminds/squirrel"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
)

var _ lakta.Module = (*Module)(nil)

type Module struct{}

func NewModule() *Module {
	return &Module{}
}

func (m *Module) Init(ctx context.Context) error {
	lakta.Provide(ctx, m.GetInstance)
	return nil
}

func (m *Module) Shutdown(_ context.Context) error {
	return nil
}

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
