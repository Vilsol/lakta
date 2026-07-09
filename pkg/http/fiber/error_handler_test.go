package fiberserver_test

import (
	stderrors "errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/gofiber/fiber/v3"
)

func TestWithErrorHandlerReachesFiberConfig(t *testing.T) {
	t.Parallel()

	sentinel := stderrors.New("sentinel")
	handler := func(_ fiber.Ctx, _ error) error { return sentinel }

	c := fiberserver.NewConfig(fiberserver.WithErrorHandler(handler))
	cfg := c.ToFiberConfig()

	testza.AssertNotNil(t, cfg.ErrorHandler)
	testza.AssertErrorIs(t, cfg.ErrorHandler(nil, nil), sentinel)
}

func TestToFiberConfigNoErrorHandlerByDefault(t *testing.T) {
	t.Parallel()

	c := fiberserver.NewConfig()
	cfg := c.ToFiberConfig()
	testza.AssertNil(t, cfg.ErrorHandler)
}
