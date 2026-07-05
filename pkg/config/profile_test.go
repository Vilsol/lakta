package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/do/v2"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProfile_PrecedenceOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\nbase_only: kept\n")
	writeFile(t, dir, "lakta.prod.yaml", "shared: prod\n")

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir), WithProfile("prod"))
	testza.AssertNil(t, m.Init(ctx))

	k := m.Koanf()
	// profile overlays base (last-wins)
	testza.AssertEqual(t, "prod", k.String("shared"))
	// base-only key survives
	testza.AssertEqual(t, "kept", k.String("base_only"))
}

func TestProfile_LaktaProfileEnvDefault(t *testing.T) {
	// Not parallel — uses t.Setenv.
	t.Setenv(envProfileVar, "prod")

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")
	writeFile(t, dir, "lakta.prod.yaml", "shared: prod\n")

	ctx := setupModuleCtx(t)
	// No WithProfile: Profile defaults from LAKTA_PROFILE.
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNil(t, m.Init(ctx))

	testza.AssertEqual(t, "prod", m.Koanf().String("shared"))
}

func TestProfile_WithProfileBeatsEnv(t *testing.T) {
	// Not parallel — uses t.Setenv.
	t.Setenv(envProfileVar, "prod")

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")
	writeFile(t, dir, "lakta.dev.yaml", "shared: dev\n")

	ctx := setupModuleCtx(t)
	// WithProfile overrides the LAKTA_PROFILE default (prod) -> dev overlay applies.
	m := NewModule(WithConfigDirs(dir), WithProfile("dev"))
	testza.AssertNil(t, m.Init(ctx))

	testza.AssertEqual(t, "dev", m.Koanf().String("shared"))
}

func TestProfile_FullStackPrecedence(t *testing.T) {
	// Not parallel — uses t.Setenv (env layer must register before flags).
	t.Setenv("LAKTATESTPROF_PORT", "3")

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "port: \"1\"\n")
	writeFile(t, dir, "lakta.prod.yaml", "port: \"2\"\n")

	ctx := setupModuleCtx(t)
	m := NewModule(
		WithConfigDirs(dir),
		WithProfile("prod"),
		WithEnvPrefix("LAKTATESTPROF_"),
		WithArgs([]string{"--port=4"}),
	)
	testza.AssertNil(t, m.Init(ctx))

	// base(1) < profile(2) < env(3) < flag(4)
	testza.AssertEqual(t, "4", m.Koanf().String("port"))
}

func TestProfile_NoProfileLoadsOnlyBase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")
	writeFile(t, dir, "lakta.prod.yaml", "shared: prod\n")

	ctx := setupModuleCtx(t)
	// Empty Profile: overlay is not consulted, base wins.
	m := NewModule(WithConfigDirs(dir))
	testza.AssertNil(t, m.Init(ctx))

	testza.AssertEqual(t, "base", m.Koanf().String("shared"))
}

func TestProfile_MissingOverlayNoError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")
	// No lakta.prod.yaml on disk.

	ctx := setupModuleCtx(t)
	m := NewModule(WithConfigDirs(dir), WithProfile("prod"))
	// Missing overlay is skipped like a missing base file — no error.
	testza.AssertNil(t, m.Init(ctx))
	testza.AssertEqual(t, "base", m.Koanf().String("shared"))
}

func TestProfile_HotReloadOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")
	writeFile(t, dir, "lakta.prod.yaml", "shared: prod\n")

	ctx := context.Background()
	injector := do.New()
	ctx = lakta.WithInjector(ctx, injector)

	m := NewModule(WithConfigDirs(dir), WithProfile("prod"))
	testza.AssertNil(t, m.Init(ctx))
	testza.AssertEqual(t, "prod", m.Koanf().String("shared"))

	// Profile file must be in the watched/reloaded set.
	found := false
	for _, cf := range m.configFiles {
		if strings.HasSuffix(cf.path, "lakta.prod.yaml") {
			found = true
		}
	}
	testza.AssertTrue(t, found)

	// Mutate the overlay and reload -> new profile value propagates.
	writeFile(t, dir, "lakta.prod.yaml", "shared: prod2\n")
	testza.AssertNil(t, m.reload())
	testza.AssertEqual(t, "prod2", m.Koanf().String("shared"))
}
