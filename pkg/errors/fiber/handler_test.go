package fiber_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	stderrors "github.com/Vilsol/lakta/pkg/errors"
	errfiber "github.com/Vilsol/lakta/pkg/errors/fiber"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

func newApp(handler fiber.Handler) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: errfiber.ErrorHandler()})
	app.Get("/", handler)
	return app
}

func doRequest(t *testing.T, app *fiber.App) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	testza.AssertNoError(t, err)
	body, err := io.ReadAll(resp.Body)
	testza.AssertNoError(t, err)
	var parsed map[string]any
	testza.AssertNoError(t, json.Unmarshal(body, &parsed))
	return resp, parsed
}

func TestErrorHandlerRendersAppError(t *testing.T) {
	t.Parallel()

	app := newApp(func(_ fiber.Ctx) error {
		return stderrors.NotFound("user not found")
	})
	resp, body := doRequest(t, app)

	testza.AssertEqual(t, http.StatusNotFound, resp.StatusCode)
	testza.AssertEqual(t, "application/problem+json", resp.Header.Get("Content-Type"))
	testza.AssertEqual(t, "urn:lakta:error:NOT_FOUND", body["type"])
	testza.AssertEqual(t, "NOT_FOUND", body["code"])
	testza.AssertEqual(t, float64(404), body["status"])
	testza.AssertEqual(t, "user not found", body["detail"])
	testza.AssertEqual(t, "Not Found", body["title"])
}

func TestErrorHandlerRendersValidationFields(t *testing.T) {
	t.Parallel()

	app := newApp(func(_ fiber.Ctx) error {
		return stderrors.Validation("invalid").
			WithField("user.email", "required").
			WithField("user.age", "min")
	})
	resp, body := doRequest(t, app)

	testza.AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	testza.AssertEqual(t, "urn:lakta:error:VALIDATION", body["type"])
	params, ok := body["invalid_params"].([]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 2, len(params))
	first, ok := params[0].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "user.email", first["name"])
	testza.AssertEqual(t, "required", first["reason"])
}

func TestErrorHandlerOmitsInvalidParamsWhenEmpty(t *testing.T) {
	t.Parallel()

	app := newApp(func(_ fiber.Ctx) error {
		return stderrors.NotFound("nope")
	})
	_, body := doRequest(t, app)

	_, present := body["invalid_params"]
	testza.AssertFalse(t, present)
}

func TestErrorHandlerInternalIsOpaque(t *testing.T) {
	t.Parallel()

	app := newApp(func(_ fiber.Ctx) error {
		return stderrors.Internal("db password is hunter2")
	})
	resp, body := doRequest(t, app)

	testza.AssertEqual(t, http.StatusInternalServerError, resp.StatusCode)
	testza.AssertEqual(t, "internal error", body["detail"])
	testza.AssertEqual(t, "urn:lakta:error:INTERNAL", body["type"])
	testza.AssertNotContains(t, body["detail"], "hunter2")
}

func TestErrorHandlerMapsRawFiberError(t *testing.T) {
	t.Parallel()

	app := newApp(func(_ fiber.Ctx) error {
		return fiber.NewError(fiber.StatusNotFound, "route missing")
	})
	resp, body := doRequest(t, app)

	testza.AssertEqual(t, http.StatusNotFound, resp.StatusCode)
	testza.AssertEqual(t, "application/problem+json", resp.Header.Get("Content-Type"))
	testza.AssertEqual(t, "urn:lakta:error:NOT_FOUND", body["type"])
}

func TestErrorHandlerPlainErrorIsInternal(t *testing.T) {
	t.Parallel()

	app := newApp(func(_ fiber.Ctx) error {
		return io.ErrUnexpectedEOF
	})
	resp, body := doRequest(t, app)

	testza.AssertEqual(t, http.StatusInternalServerError, resp.StatusCode)
	testza.AssertEqual(t, "internal error", body["detail"])
	testza.AssertNotContains(t, body["detail"], "EOF")
}

func TestErrorHandlerNeverReflectsRequestBody(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{ErrorHandler: errfiber.ErrorHandler()})
	app.Post("/", func(_ fiber.Ctx) error {
		return stderrors.InvalidArgument("bad input")
	})

	secret := "SUPER_SECRET_BODY_TOKEN"
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(secret))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := app.Test(req)
	testza.AssertNoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	testza.AssertNoError(t, err)
	testza.AssertNotContains(t, string(raw), secret)
}

func TestErrorHandlerRecoversPanic(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{ErrorHandler: errfiber.ErrorHandler()})
	app.Use(recover.New())
	app.Get("/", func(_ fiber.Ctx) error {
		panic("db password is hunter2")
	})

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, http.StatusInternalServerError, resp.StatusCode)
	testza.AssertEqual(t, "application/problem+json", resp.Header.Get("Content-Type"))

	raw, err := io.ReadAll(resp.Body)
	testza.AssertNoError(t, err)
	testza.AssertNotContains(t, string(raw), "hunter2")

	var body map[string]any
	testza.AssertNoError(t, json.Unmarshal(raw, &body))
	testza.AssertEqual(t, "INTERNAL", body["code"])
	testza.AssertEqual(t, "urn:lakta:error:INTERNAL", body["type"])
	testza.AssertEqual(t, "internal error", body["detail"])
}
