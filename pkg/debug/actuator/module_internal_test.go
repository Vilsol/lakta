package actuator

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/Vilsol/lakta/pkg/lakta"
	slogmod "github.com/Vilsol/lakta/pkg/logging/slog"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
	"github.com/hellofresh/health-go/v5"
)

type fakeLevelController struct{ lvl slog.Level }

func (f *fakeLevelController) SetLevel(l slog.Level) { f.lvl = l }
func (f *fakeLevelController) Level() slog.Level     { return f.lvl }

type demoService struct{}

func decodeJSON[T any](t *testing.T, resp *http.Response) T { //nolint:ireturn
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	testza.AssertNoError(t, err)
	var out T
	testza.AssertNoError(t, json.Unmarshal(body, &out))
	return out
}

func TestConfigEndpointRedactsSecret(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t).WithData(map[string]any{
		"database": map[string]any{
			"host":     "localhost", //nolint:goconst
			"password": "s3cret",    //nolint:goconst
		},
	})

	act := NewModule(WithEnabled(true))
	testza.AssertNoError(t, act.Init(h.Ctx()))

	resp, err := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/config", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	out := decodeJSON[ConfigResponse](t, resp)
	db, ok := out.Values["database"].(map[string]any)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, redactMask, db["password"])
	testza.AssertEqual(t, "localhost", db["host"])
}

func TestRoutesEndpointAggregatesInstances(t *testing.T) {
	t.Parallel()

	reg := fiberserver.NewRoutesRegistry()
	reg.Append(fiberserver.RoutesSnapshot{Instance: "public", Routes: []fiber.Route{{Method: "GET", Path: "/pub"}}})
	reg.Append(fiberserver.RoutesSnapshot{Instance: "internal", Routes: []fiber.Route{{Method: "POST", Path: "/int"}}})

	h := testkit.NewHarness(t)
	lakta.ProvideValue(h.Ctx(), reg)

	act := NewModule(WithEnabled(true))
	testza.AssertNoError(t, act.Init(h.Ctx()))

	resp, err := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/routes", nil))
	testza.AssertNoError(t, err)

	out := decodeJSON[RoutesResponse](t, resp)
	testza.AssertEqual(t, 2, len(out.Instances))

	names := map[string]bool{}
	for _, inst := range out.Instances {
		names[inst.Instance] = true
	}
	testza.AssertTrue(t, names["public"])
	testza.AssertTrue(t, names["internal"])
}

func TestInfoEndpoint(t *testing.T) {
	t.Parallel()

	act, err := initModule(t, WithEnabled(true))
	testza.AssertNoError(t, err)

	resp, testErr := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/info", nil))
	testza.AssertNoError(t, testErr)
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	out := decodeJSON[InfoResponse](t, resp)
	testza.AssertNotEqual(t, "", out.GoVersion)
}

func TestHealthEndpointProxyAndAbsent(t *testing.T) {
	t.Parallel()

	// Absent: 501.
	act, err := initModule(t, WithEnabled(true))
	testza.AssertNoError(t, err)
	resp, testErr := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/health", nil))
	testza.AssertNoError(t, testErr)
	testza.AssertEqual(t, http.StatusNotImplemented, resp.StatusCode)

	// Present: proxied.
	hh, hErr := health.New()
	testza.AssertNoError(t, hErr)
	h := testkit.NewHarness(t)
	lakta.ProvideValue(h.Ctx(), hh)
	act2 := NewModule(WithEnabled(true))
	testza.AssertNoError(t, act2.Init(h.Ctx()))
	resp2, testErr2 := act2.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/health", nil))
	testza.AssertNoError(t, testErr2)
	testza.AssertEqual(t, http.StatusOK, resp2.StatusCode)
}

func TestLoggersEndpointFlipsLevel(t *testing.T) {
	t.Parallel()

	fake := &fakeLevelController{lvl: slog.LevelInfo}
	h := testkit.NewHarness(t)
	lakta.ProvideValue[slogmod.LevelController](h.Ctx(), fake)

	act := NewModule(WithEnabled(true), WithAuth(passAuth))
	testza.AssertNoError(t, act.Init(h.Ctx()))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/debug/loggers", strings.NewReader("level=debug"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := act.app.Test(req)
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	out := decodeJSON[LoggersResponse](t, resp)
	testza.AssertEqual(t, "debug", out.Level)
	testza.AssertEqual(t, slog.LevelDebug, fake.lvl)
}

func TestDIEndpointRendersGraph(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	lakta.ProvideValue(h.Ctx(), &demoService{})

	act := NewModule(WithEnabled(true))
	testza.AssertNoError(t, act.Init(h.Ctx()))

	resp, err := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/di", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	out := decodeJSON[DIResponse](t, resp)
	testza.AssertEqual(t, "mermaid", out.Format)
	testza.AssertTrue(t, strings.Contains(out.Graph, "demoService"))
}

func TestModulesAndStartupEndpoints(t *testing.T) {
	t.Parallel()

	act := NewModule(WithEnabled(true), WithHost("127.0.0.1"), WithPort(0))
	mock := testkit.NewMockModule()

	rh := testkit.NewRuntimeHarness(t, mock, act)
	testkit.WaitForAddr(t, act)

	resp, err := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/modules", nil))
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	modules := decodeJSON[ModulesResponse](t, resp)
	testza.AssertTrue(t, len(modules.Modules) >= 2)

	foundActuator := false
	for _, mv := range modules.Modules {
		testza.AssertNotEqual(t, "", mv.State)
		testza.AssertNotEqual(t, "", mv.Lifecycle)
		if strings.Contains(mv.Type, "actuator") {
			foundActuator = true
		}
	}
	testza.AssertTrue(t, foundActuator)

	resp2, err2 := act.app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/startup", nil))
	testza.AssertNoError(t, err2)
	startup := decodeJSON[StartupResponse](t, resp2)
	testza.AssertTrue(t, len(startup.Entries) >= 2)

	_ = rh
}

func TestDisabledModuleInert(t *testing.T) {
	t.Parallel()

	act, err := initModule(t)
	testza.AssertNoError(t, err)
	testza.AssertNil(t, act.app)
	testza.AssertNil(t, act.Addr())
}
