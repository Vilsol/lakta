package temporal_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/MarvinJWendt/testza"
	pkgtemporal "github.com/Vilsol/lakta/pkg/workflows/temporal"
	"github.com/knadh/koanf/v2"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"google.golang.org/grpc/credentials"
)

func TestConfig_LoadFromKoanf(t *testing.T) {
	t.Parallel()

	const path = "modules.workflows.temporal.default"

	k := koanf.New(".")
	testza.AssertNil(t, k.Set(path+".target", "temporal.example:7233"))
	testza.AssertNil(t, k.Set(path+".task_queue", "orders"))
	testza.AssertNil(t, k.Set(path+".namespace", "production"))
	testza.AssertNil(t, k.Set(path+".insecure", true))

	cfg := pkgtemporal.NewDefaultConfig()
	testza.AssertNil(t, cfg.LoadFromKoanf(k, path))

	testza.AssertEqual(t, "temporal.example:7233", cfg.Target)
	testza.AssertEqual(t, "orders", cfg.TaskQueue)
	testza.AssertEqual(t, "production", cfg.Namespace)
	testza.AssertTrue(t, cfg.Insecure)
}

func TestConfig_GetCredentials_Insecure(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewConfig(pkgtemporal.WithInsecure(true))
	testza.AssertNotNil(t, cfg.GetCredentials())
}

func TestConfig_GetCredentials_DefaultNil(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewDefaultConfig()
	testza.AssertNil(t, cfg.GetCredentials())
}

func TestConfig_GetCredentials_ExplicitTLS(t *testing.T) {
	t.Parallel()

	tlsCreds := credentials.NewClientTLSFromCert(nil, "")
	cfg := pkgtemporal.NewConfig()
	cfg.Credentials = tlsCreds

	got := cfg.GetCredentials()
	testza.AssertNotNil(t, got)
	testza.AssertEqual(t, tlsCreds, got)
}

func TestConfig_GetCredentials_ExplicitOverridesInsecure(t *testing.T) {
	t.Parallel()

	tlsCreds := credentials.NewClientTLSFromCert(nil, "")
	cfg := pkgtemporal.NewConfig(pkgtemporal.WithInsecure(true))
	cfg.Credentials = tlsCreds

	testza.AssertEqual(t, tlsCreds, cfg.GetCredentials())
}

func TestConfig_DialOptions_Insecure(t *testing.T) {
	t.Parallel()

	insecureCfg := pkgtemporal.NewConfig(pkgtemporal.WithInsecure(true))
	defaultCfg := pkgtemporal.NewDefaultConfig()

	// Insecure adds transport credentials; default (nil creds) does not.
	testza.AssertNotNil(t, insecureCfg.DialOptions())
	testza.AssertNotNil(t, defaultCfg.DialOptions())
	testza.AssertTrue(t, len(insecureCfg.DialOptions()) > len(defaultCfg.DialOptions()))
}

func TestConfig_DialOptions_TLS(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewConfig()
	cfg.Credentials = credentials.NewClientTLSFromCert(nil, "")

	defaultCfg := pkgtemporal.NewDefaultConfig()

	// TLS creds add a transport-credentials dial option beyond the default stats handler.
	testza.AssertTrue(t, len(cfg.DialOptions()) > len(defaultCfg.DialOptions()))
}

func TestConfig_ClientOptions(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewConfig(
		pkgtemporal.WithTarget("temporal:7233"),
		pkgtemporal.WithNamespace("ns1"),
		pkgtemporal.WithInsecure(true),
	)

	logger := log.NewStructuredLogger(slog.New(slog.DiscardHandler))
	interceptors := []interceptor.ClientInterceptor{}
	opts := cfg.ClientOptions(logger, interceptors)

	testza.AssertEqual(t, "temporal:7233", opts.HostPort)
	testza.AssertEqual(t, "ns1", opts.Namespace)
	testza.AssertNotNil(t, opts.Logger)
	testza.AssertNotNil(t, opts.ConnectionOptions.DialOptions)
	testza.AssertTrue(t, len(opts.ConnectionOptions.DialOptions) > 0)
}

func TestConfig_WorkerOptions(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewDefaultConfig()
	ctx := context.Background()
	interceptors := []interceptor.WorkerInterceptor{}

	opts := cfg.WorkerOptions(ctx, interceptors)

	testza.AssertEqual(t, ctx, opts.BackgroundActivityContext)
	testza.AssertNotNil(t, opts.Interceptors)
}

func TestConfig_WithOptions(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewConfig(
		pkgtemporal.WithName("custom"),
		pkgtemporal.WithTarget("host:1234"),
		pkgtemporal.WithTaskQueue("queue"),
		pkgtemporal.WithNamespace("ns"),
		pkgtemporal.WithInsecure(true),
	)

	testza.AssertEqual(t, "custom", cfg.Name)
	testza.AssertEqual(t, "host:1234", cfg.Target)
	testza.AssertEqual(t, "queue", cfg.TaskQueue)
	testza.AssertEqual(t, "ns", cfg.Namespace)
	testza.AssertTrue(t, cfg.Insecure)
}

func TestConfig_WithRegistrar(t *testing.T) {
	t.Parallel()

	cfg := pkgtemporal.NewConfig(
		pkgtemporal.WithRegistrar(func(context.Context, worker.Worker) error { return nil }),
		pkgtemporal.WithRegistrar(func(context.Context, worker.Worker) error { return nil }),
	)

	testza.AssertEqual(t, 2, len(cfg.Registrars))
}
