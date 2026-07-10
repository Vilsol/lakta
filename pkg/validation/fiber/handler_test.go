package fiber_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	errfiber "github.com/Vilsol/lakta/pkg/errors/fiber"
	valfiber "github.com/Vilsol/lakta/pkg/validation/fiber"
	"github.com/gofiber/fiber/v3"
)

type signupDTO struct {
	User userDTO `json:"user"`
}

type userDTO struct {
	Email string `json:"email" validate:"required,email"`
}

type problemBody struct {
	Status        int    `json:"status"`
	Code          string `json:"code"`
	InvalidParams []struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	} `json:"invalid_params"`
}

func newSignupApp() *fiber.App {
	app := fiber.New(fiber.Config{
		StructValidator: valfiber.New(),
		ErrorHandler:    errfiber.ErrorHandler(),
	})
	app.Post("/signup", func(c fiber.Ctx) error {
		var dto signupDTO
		if err := c.Bind().Body(&dto); err != nil {
			return err //nolint:wrapcheck // propagate the validator AppError to the ErrorHandler unchanged
		}
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func post(t *testing.T, app *fiber.App, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	testza.AssertNoError(t, err)
	return resp
}

func TestBadEmailRendersProblemJSON(t *testing.T) {
	t.Parallel()

	resp := post(t, newSignupApp(), `{"user":{"email":"not-an-email"}}`)

	testza.AssertEqual(t, http.StatusBadRequest, resp.StatusCode)
	testza.AssertEqual(t, "application/problem+json", resp.Header.Get("Content-Type"))

	raw, err := io.ReadAll(resp.Body)
	testza.AssertNoError(t, err)

	var body problemBody
	testza.AssertNoError(t, json.Unmarshal(raw, &body))
	testza.AssertEqual(t, http.StatusBadRequest, body.Status)
	testza.AssertEqual(t, "VALIDATION", body.Code)

	got := map[string]string{}
	for _, p := range body.InvalidParams {
		got[p.Name] = p.Reason
	}
	testza.AssertEqual(t, "email", got["user.email"])
}

func TestValidBodyReachesHandler(t *testing.T) {
	t.Parallel()

	resp := post(t, newSignupApp(), `{"user":{"email":"a@b.com"}}`)
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)
}
