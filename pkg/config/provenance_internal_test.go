package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func TestProvenanceSnapshot(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	dir := t.TempDir()
	testza.AssertNoError(t, os.WriteFile(
		filepath.Join(dir, "lakta.yaml"),
		[]byte("filekey: fileval\n"),
		0o600,
	))

	t.Setenv("LAKTA_ENVKEY", "envval")
	// Seed the flag key via env so posflag registers it, then override via flag.
	t.Setenv("LAKTA_FLAGKEY", "envseed")

	ctx := setupModuleCtx(t)
	m := NewModule(
		WithConfigDirs(dir),
		WithConfigName("lakta"),
		WithArgs([]string{"--flagkey=flagval"}),
	)
	testza.AssertNil(t, m.Init(ctx))

	// A key present only in the merged koanf (set outside any layer) is default.
	testza.AssertNil(t, m.koanf.Set("synthetic", "x"))

	origins := map[string]string{}
	values := map[string]any{}
	for _, e := range m.ProvenanceSnapshot() {
		origins[e.Key] = e.Origin
		values[e.Key] = e.Value
	}

	testza.AssertEqual(t, OriginFile, origins["filekey"])
	testza.AssertEqual(t, OriginEnv, origins["envkey"])
	testza.AssertEqual(t, OriginFlag, origins["flagkey"])
	testza.AssertEqual(t, OriginDefault, origins["synthetic"])

	// Flag layer wins over the env seed.
	testza.AssertEqual(t, "flagval", values["flagkey"])
}
