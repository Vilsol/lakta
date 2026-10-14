package management_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/http/management"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/hellofresh/health-go/v5"
	"github.com/prometheus/client_golang/prometheus"
)

type deps struct {
	health   *health.Health
	registry *prometheus.Registry
	failing  atomic.Bool
}

func start(t *testing.T) (string, *deps) {
	t.Helper()

	d := &deps{registry: prometheus.NewRegistry()}
	h, err := health.New()
	testza.AssertNil(t, err)
	d.health = h

	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "mgmt_test_total", Help: "Test counter."})
	counter.Inc()
	d.registry.MustRegister(counter)

	provider := testkit.NewMockModule()
	provider.OnInit = func(ctx context.Context) error {
		lakta.ProvideValue(ctx, d.health)
		lakta.ProvideValue(ctx, d.registry)
		return nil
	}

	m := management.NewModule(management.WithHost("127.0.0.1"), management.WithPort(0))
	testkit.NewRuntimeHarness(t, provider, m)

	return "http://" + testkit.WaitForAddr(t, m).String(), d
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	testza.AssertNil(t, err)
	resp, err := http.DefaultClient.Do(req)
	testza.AssertNil(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	testza.AssertNil(t, err)
	return resp.StatusCode, string(body)
}

func TestHealth_AlwaysOKWhenServing(t *testing.T) {
	t.Parallel()

	base, d := start(t)
	testza.AssertNil(t, d.health.Register(health.Config{Name: "down", Check: func(context.Context) error { return errors.New("down") }}))

	code, _ := get(t, base+"/health")
	testza.AssertEqual(t, http.StatusOK, code)
}

func TestReady_ReflectsLateRegisteredChecks(t *testing.T) {
	t.Parallel()

	base, d := start(t)

	code, _ := get(t, base+"/ready")
	testza.AssertEqual(t, http.StatusOK, code)

	testza.AssertNil(t, d.health.Register(health.Config{Name: "dep", Check: func(context.Context) error {
		if d.failing.Load() {
			return errors.New("dep down")
		}
		return nil
	}}))

	d.failing.Store(true)
	code, _ = get(t, base+"/ready")
	testza.AssertEqual(t, http.StatusServiceUnavailable, code)

	d.failing.Store(false)
	code, _ = get(t, base+"/ready")
	testza.AssertEqual(t, http.StatusOK, code)
}

func TestMetrics_ServesPrivateRegistry(t *testing.T) {
	t.Parallel()

	base, _ := start(t)

	code, body := get(t, base+"/metrics")
	testza.AssertEqual(t, http.StatusOK, code)
	testza.AssertContains(t, body, "mgmt_test_total 1")
}

func TestConfigPath(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.http.management.default", management.NewModule().ConfigPath())
}
