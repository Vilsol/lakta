package fiberserver_test

import (
	stderrors "errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
)

type stubValidator struct{ err error }

func (s stubValidator) Validate(any) error { return s.err }

func TestWithStructValidatorReachesFiberConfig(t *testing.T) {
	t.Parallel()

	sentinel := stderrors.New("sentinel")
	c := fiberserver.NewConfig(fiberserver.WithStructValidator(stubValidator{err: sentinel}))
	cfg := c.ToFiberConfig()

	testza.AssertNotNil(t, cfg.StructValidator)
	testza.AssertErrorIs(t, cfg.StructValidator.Validate(nil), sentinel)
}

func TestToFiberConfigNoStructValidatorByDefault(t *testing.T) {
	t.Parallel()

	c := fiberserver.NewConfig()
	cfg := c.ToFiberConfig()
	testza.AssertNil(t, cfg.StructValidator)
}
