package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
)

var (
	_ lakta.Module   = (*Module)(nil)
	_ lakta.Provider = (*Module)(nil)
)

func setupModuleCtx(t *testing.T) context.Context {
	t.Helper()
	injector := do.New()
	return lakta.WithInjector(context.Background(), injector)
}

func TestConfigModule_ProvidesKoanf(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"))

	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, k)
}

func TestConfigModule_ProvidesReloadNotifier(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"))

	testza.AssertNil(t, m.Init(ctx))

	n, err := do.Invoke[ReloadNotifier](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, n)
}

func TestConfigModule_EnvVarOverride(t *testing.T) {
	// Not parallel — t.Setenv requires sequential execution.
	t.Setenv("LAKTATEST_FOO", "bar")

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"), WithEnvPrefix("LAKTATEST_"))

	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "bar", k.String("foo"))
}

func TestConfigModule_CLIFlagOverride(t *testing.T) {
	// Not parallel — uses t.Setenv to pre-seed a koanf key so CLI can override it.
	// (pflag only overrides keys it has registered; env var pre-population registers the key.)
	t.Setenv("LAKTATESTCLI_SOME_KEY", "initial")

	ctx := setupModuleCtx(t)
	m := NewModule(
		WithConfigDirs("/nonexistent"),
		WithEnvPrefix("LAKTATESTCLI_"),
		WithArgs([]string{"--some.key=override"}),
	)

	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "override", k.String("some.key"))
}

func TestConfigModule_NoConfigFilesOK(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent/path/that/does/not/exist"))

	testza.AssertNil(t, m.Init(ctx))
}

func TestConfigModule_ReloadFiresCallbacks(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"))
	testza.AssertNil(t, m.Init(ctx))

	var received *koanf.Koanf
	m.OnReload(func(k *koanf.Koanf) {
		received = k
	})

	testza.AssertNil(t, m.reload())
	testza.AssertNotNil(t, received)
}

func TestConfigModule_ReloadUpdatesKoanf(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"), WithEnvPrefix("LAKTATESTNOTSET_"))

	testza.AssertNil(t, m.Init(ctx))

	original := m.Koanf()
	testza.AssertNil(t, m.reload())
	reloaded := m.Koanf()

	testza.AssertFalse(t, original == reloaded)
}

func TestConfigModule_Shutdown(t *testing.T) {
	t.Parallel()

	m := NewModule(WithConfigDirs("/nonexistent"))
	testza.AssertNil(t, m.Shutdown(context.Background()))
}

func TestConfigModule_IsConfigModule(t *testing.T) {
	t.Parallel()

	m := NewModule()
	m.IsConfigModule() // marker method — must not panic
}

func TestNewDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := NewDefaultConfig()
	testza.AssertEqual(t, defaultEnvPrefix, cfg.EnvPrefix)
	testza.AssertEqual(t, defaultConfigName, cfg.ConfigName)
	testza.AssertNotNil(t, cfg.ConfigDirs)
}

func TestWithConfigName(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(WithConfigName("myapp"))
	testza.AssertEqual(t, "myapp", cfg.ConfigName)
}

func TestParseFlag_ValidFlag(t *testing.T) {
	t.Parallel()

	k, v, ok := parseFlag("--some.key=val")
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "some.key", k)
	testza.AssertEqual(t, "val", v)
}

func TestParseFlag_NoPrefix(t *testing.T) {
	t.Parallel()

	_, _, ok := parseFlag("some.key=val")
	testza.AssertFalse(t, ok)
}

func TestParseFlag_NoEquals(t *testing.T) {
	t.Parallel()

	_, _, ok := parseFlag("--somekey")
	testza.AssertFalse(t, ok)
}

func TestConfigModule_Provides(t *testing.T) {
	t.Parallel()

	m := NewModule()
	types := m.Provides()
	testza.AssertEqual(t, 2, len(types))
	testza.AssertTrue(t, types[0] == reflect.TypeFor[*koanf.Koanf]())
	testza.AssertTrue(t, types[1] == reflect.TypeFor[ReloadNotifier]())
}

func TestUnmarshalKoanf(t *testing.T) {
	t.Parallel()

	k := koanf.New(".")
	testza.AssertNil(t, k.Load(mapProvider(map[string]any{
		"app": map[string]any{"port": 8080},
	}), nil))

	type cfg struct {
		Port int `koanf:"port"`
	}
	var c cfg
	testza.AssertNil(t, UnmarshalKoanf(&c, k, "app"))
	testza.AssertEqual(t, 8080, c.Port)
}

func TestConfigModule_LoadsYAMLFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("port: 9090\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, int64(9090), k.Int64("port"))
}

func TestConfigModule_LoadsJSONFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.json"), []byte(`{"host":"localhost"}`), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "localhost", k.String("host"))
}

func TestConfigModule_LoadsTOMLFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.toml"), []byte("debug = true\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertTrue(t, k.Bool("debug"))
}

func TestConfigModule_InvalidConfigFileReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte(":\tinvalid: yaml: [\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNotNil(t, m.Init(ctx))
}

func TestConfigModule_Reload_FileGoneReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lakta.yaml")
	testza.AssertNil(t, os.WriteFile(cfgPath, []byte("x: 1\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNil(t, m.Init(ctx))

	testza.AssertNil(t, os.Remove(cfgPath))
	testza.AssertNotNil(t, m.reload())
}

func TestConfigModule_Reload_WithCLIFlags(t *testing.T) {
	// Not parallel — uses t.Setenv.
	t.Setenv("LAKTATESTFLAGR_KEY", "initial")

	ctx := setupModuleCtx(t)
	m := NewModule(
		WithConfigDirs("/nonexistent"),
		WithEnvPrefix("LAKTATESTFLAGR_"),
		WithArgs([]string{"--key=override"}),
	)
	testza.AssertNil(t, m.Init(ctx))
	testza.AssertNil(t, m.reload())

	testza.AssertEqual(t, "override", m.Koanf().String("key"))
}

func TestConfigModule_CLIFlags_UnknownFlagPath(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	// Args after "--" are positional and end up in flagSet.Args() as-is.
	// The manual parseFlag loop picks up any --key=value entries from there.
	m := NewModule(
		WithConfigDirs("/nonexistent"),
		WithArgs([]string{"--", "--unregistered.key=injected"}),
	)
	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "injected", k.String("unregistered.key"))
}

func TestConfigModule_CLIFlags_IntType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// YAML parser gives int64 for integer literals, exercising the int64 branch.
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("workers: 4\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir), WithArgs([]string{"--workers=8"}))
	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, int64(8), k.Int64("workers"))
}

func TestConfigModule_CLIFlags_BoolType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("debug: false\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir), WithArgs([]string{"--debug=true"}))
	testza.AssertNil(t, m.Init(ctx))

	k, err := do.Invoke[*koanf.Koanf](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertTrue(t, k.Bool("debug"))
}

// mapProvider is a minimal koanf.Provider backed by a map, used in tests.
type mapProvider map[string]any

func (m mapProvider) Load() (map[string]any, error) { return m, nil }
func (m mapProvider) ReadBytes() ([]byte, error)    { return nil, nil }
func (m mapProvider) Read() (map[string]any, error) { return m, nil }
