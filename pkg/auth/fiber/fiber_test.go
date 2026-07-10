package fiber_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	authfiber "github.com/Vilsol/lakta/pkg/auth/fiber"
	"github.com/Vilsol/lakta/pkg/auth/verifier"
	errfiber "github.com/Vilsol/lakta/pkg/errors/fiber"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const testSecret = "fiber-adapter-dev-secret" //nolint:gosec // test-only fixture secret

const scopeRead = "read"

func TestMain(m *testing.M) {
	_ = os.Setenv("LAKTA_PROFILE", "test")
	os.Exit(m.Run())
}

func testRegistry(t *testing.T) *verifier.Registry {
	t.Helper()
	h := testkit.NewHarness(t)

	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		"modules.auth.verifier.default.static_key.secret":   testSecret,
		"modules.auth.verifier.default.static_key.profiles": []string{"test"},
		"modules.auth.verifier.default.scope_claim":         "scope",
		"modules.auth.verifier.default.roles_claim":         "roles",
	}, "."), nil))

	m := verifier.NewModule()
	testza.AssertNoError(t, m.LoadConfig(k))
	testza.AssertNoError(t, m.Init(h.Ctx()))
	t.Cleanup(func() { _ = m.Shutdown(h.Ctx()) })

	reg, err := lakta.Invoke[*verifier.Registry](h.Ctx())
	testza.AssertNoError(t, err)
	return reg
}

func token(t *testing.T, scopes, roles []string) string {
	t.Helper()
	now := time.Now()
	b := jwt.NewBuilder().Issuer("dev-cli").Subject("user-1").
		IssuedAt(now).Expiration(now.Add(time.Hour))
	if scopes != nil {
		b = b.Claim("scope", joinSpace(scopes))
	}
	if roles != nil {
		b = b.Claim("roles", toAny(roles))
	}
	tok, err := b.Build()
	testza.AssertNoError(t, err)
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.HS256(), []byte(testSecret)))
	testza.AssertNoError(t, err)
	return string(signed)
}

func joinSpace(in []string) string {
	out := ""
	var outSb72 strings.Builder
	for i, s := range in {
		if i > 0 {
			outSb72.WriteString(" ")
		}
		outSb72.WriteString(s)
	}
	out += outSb72.String()
	return out
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func newApp(t *testing.T, reg *verifier.Registry) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: errfiber.ErrorHandler()})
	guard, err := authfiber.New(reg, "default")
	testza.AssertNoError(t, err)

	app.Get("/me", guard, func(c fiber.Ctx) error {
		p, ok := verifier.PrincipalFrom(c.Context())
		testza.AssertTrue(t, ok)
		return c.SendString(p.Subject)
	})
	app.Get("/admin", guard, authfiber.RequireScope("admin"), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/role", guard, authfiber.RequireRole("editor"), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func do(t *testing.T, app *fiber.App, path, bearer string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req)
	testza.AssertNoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	testza.AssertNoError(t, err)
	return resp.StatusCode, string(body)
}

func TestNew_SetsPrincipal(t *testing.T) {
	t.Parallel()
	app := newApp(t, testRegistry(t))
	status, body := do(t, app, "/me", token(t, []string{scopeRead}, nil))
	testza.AssertEqual(t, fiber.StatusOK, status)
	testza.AssertEqual(t, "user-1", body)
}

func TestNew_NoToken_401(t *testing.T) {
	t.Parallel()
	app := newApp(t, testRegistry(t))
	status, body := do(t, app, "/me", "")
	testza.AssertEqual(t, fiber.StatusUnauthorized, status)
	testza.AssertContains(t, body, "authentication required")
}

func TestNew_BadToken_401(t *testing.T) {
	t.Parallel()
	app := newApp(t, testRegistry(t))
	status, _ := do(t, app, "/me", "garbage.token.here")
	testza.AssertEqual(t, fiber.StatusUnauthorized, status)
}

func TestRequireScope_403_WhenPrincipalLacksScope(t *testing.T) {
	t.Parallel()
	app := newApp(t, testRegistry(t))
	status, body := do(t, app, "/admin", token(t, []string{scopeRead}, nil))
	testza.AssertEqual(t, fiber.StatusForbidden, status)
	testza.AssertContains(t, body, "permission denied")
}

func TestRequireScope_200_WhenPrincipalHasScope(t *testing.T) {
	t.Parallel()
	app := newApp(t, testRegistry(t))
	status, _ := do(t, app, "/admin", token(t, []string{"admin"}, nil))
	testza.AssertEqual(t, fiber.StatusOK, status)
}

func TestRequireScope_401_WhenNoPrincipal(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	// /admin without New in front: RequireScope alone must 401 (no principal).
	app := fiber.New(fiber.Config{ErrorHandler: errfiber.ErrorHandler()})
	app.Get("/bare", authfiber.RequireScope("admin"), func(c fiber.Ctx) error { return c.SendString("ok") })
	_ = reg
	status, _ := do(t, app, "/bare", "")
	testza.AssertEqual(t, fiber.StatusUnauthorized, status)
}

func TestRequireRole_403_vs_200(t *testing.T) {
	t.Parallel()
	app := newApp(t, testRegistry(t))
	s1, _ := do(t, app, "/role", token(t, nil, []string{"viewer"}))
	testza.AssertEqual(t, fiber.StatusForbidden, s1)
	s2, _ := do(t, app, "/role", token(t, nil, []string{"editor"}))
	testza.AssertEqual(t, fiber.StatusOK, s2)
}

func TestOptional_AllowsAnonymous(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	app := fiber.New(fiber.Config{ErrorHandler: errfiber.ErrorHandler()})
	opt, err := authfiber.Optional(reg, "default")
	testza.AssertNoError(t, err)
	app.Get("/opt", opt, func(c fiber.Ctx) error {
		if p, ok := verifier.PrincipalFrom(c.Context()); ok {
			return c.SendString(p.Subject)
		}
		return c.SendString("anonymous")
	})
	s1, b1 := do(t, app, "/opt", "")
	testza.AssertEqual(t, fiber.StatusOK, s1)
	testza.AssertEqual(t, "anonymous", b1)
	s2, b2 := do(t, app, "/opt", token(t, []string{scopeRead}, nil))
	testza.AssertEqual(t, fiber.StatusOK, s2)
	testza.AssertEqual(t, "user-1", b2)
}
