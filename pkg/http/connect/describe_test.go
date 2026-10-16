package connect_test

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	"github.com/Vilsol/lakta/pkg/lakta"
)

func describe(t *testing.T, dir string) []lakta.DescribedConfig {
	t.Helper()
	var got []lakta.DescribedConfig
	err := lakta.NewRuntime(config.NewModule(config.WithConfigDirs(dir)), connectmod.NewModule()).
		WithDescriber(func(_ io.Writer, entries []lakta.DescribedConfig) error {
			got = entries
			return nil
		}).
		RunContext(t.Context())
	testza.AssertNil(t, err)
	return got
}

func TestDescribe_ReportsFileValuesWithoutListening(t *testing.T) {
	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	port := lis.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // tcp listener

	dir := t.TempDir()
	body := "modules:\n  http:\n    connect:\n      default:\n        port: " + strconv.Itoa(port) + "\n"
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte(body), 0o600))

	t.Setenv(lakta.DescribeEnv, "config")
	got := describe(t, dir)

	testza.AssertLen(t, got, 1)
	testza.AssertEqual(t, "modules.http.connect.default", got[0].Path)
	cfg, ok := got[0].Value.(connectmod.Config)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, uint16(port), cfg.Port) //nolint:gosec // test port fits
}

func TestDescribe_IgnoresEnvironment(t *testing.T) {
	dir := t.TempDir()
	testza.AssertNil(
		t,
		os.WriteFile(filepath.Join(dir, "lakta.prod.yaml"), []byte("modules:\n  http:\n    connect:\n      default:\n        port: 1\n"), 0o600),
	)
	t.Setenv(lakta.DescribeEnv, "config")
	plain := describe(t, dir)

	t.Setenv("LAKTA_PROFILE", "prod")
	t.Setenv("LAKTA_MODULES__HTTP__CONNECT__DEFAULT__PORT", "2")
	testza.AssertEqual(t, plain, describe(t, dir))
}
