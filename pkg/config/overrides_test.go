package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
)

func TestWithValues_BeatsFilesEnvAndFlags(t *testing.T) {
	// Not parallel — t.Setenv requires sequential execution.
	t.Setenv("LAKTAOVR_APP__NAME", "from-env")

	dir := t.TempDir()
	testza.AssertNil(t, os.WriteFile(filepath.Join(dir, "lakta.yaml"), []byte("app:\n  name: from-file\n  port: 1\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(
		WithConfigDirs(dir),
		WithEnvPrefix("LAKTAOVR_"),
		WithArgs([]string{"--app.name=from-flag"}),
		WithValues(map[string]any{"app.name": "from-override", "app.jwks": "http://127.0.0.1:1"}),
	)
	testza.AssertNil(t, m.Init(ctx))

	k := m.Koanf()
	testza.AssertEqual(t, "from-override", k.String("app.name"))
	testza.AssertEqual(t, "http://127.0.0.1:1", k.String("app.jwks"))
	testza.AssertEqual(t, 1, k.Int("app.port"))
}

func TestWithValues_SurvivesReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "lakta.yaml")
	testza.AssertNil(t, os.WriteFile(path, []byte("app:\n  name: a\n"), 0o600))

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir), WithValues(map[string]any{"app.socket": "/tmp/s.sock"}))
	testza.AssertNil(t, m.Init(ctx))

	testza.AssertNil(t, os.WriteFile(path, []byte("app:\n  name: b\n"), 0o600))
	testza.AssertNil(t, m.reload())

	testza.AssertEqual(t, "b", m.Koanf().String("app.name"))
	testza.AssertEqual(t, "/tmp/s.sock", m.Koanf().String("app.socket"))
}

func TestWithValues_Provenance(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"), WithValues(map[string]any{"app.x": 1}))
	testza.AssertNil(t, m.Init(ctx))

	var origin string
	for _, e := range m.ProvenanceSnapshot() {
		if e.Key == "app.x" {
			origin = e.Origin
		}
	}
	testza.AssertEqual(t, OriginOverride, origin)
}

func TestProfile_ProvidedInDI(t *testing.T) {
	t.Parallel()

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs("/nonexistent"), WithProfile("dev"))
	testza.AssertNil(t, m.Init(ctx))

	p, err := do.Invoke[Profile](lakta.GetInjector(ctx))
	testza.AssertNil(t, err)
	testza.AssertEqual(t, Profile("dev"), p)
	testza.AssertEqual(t, Profile("dev"), m.Profile())
}

func TestProfile_IsProduction(t *testing.T) {
	t.Parallel()

	testza.AssertFalse(t, Profile("dev").IsProduction())
	testza.AssertFalse(t, Profile("test").IsProduction())
	testza.AssertTrue(t, Profile("prod").IsProduction())
	testza.AssertTrue(t, Profile("preprod").IsProduction())
	testza.AssertTrue(t, Profile("").IsProduction())
}
