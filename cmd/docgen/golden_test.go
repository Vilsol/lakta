package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/cmd/internal/reflectcfg"
)

// chdirRepoRoot points the test at the workspace root so extractComments can
// resolve package source dirs and ParseGoMod finds go.work, matching what
// `mise run docgen` / `mise run schema` do.
func chdirRepoRoot(t *testing.T) {
	t.Helper()

	dir, err := os.Getwd()
	testza.AssertNoError(t, err)

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.work")); statErr == nil {
			t.Chdir(dir)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.work not found walking up from test dir")
		}
		dir = parent
	}
}

// TestDocgenYAMLByteIdentical asserts the reflectcfg refactor keeps docs.yaml
// byte-for-byte what the old inlined pipeline produced. This is the same
// invariant docgen-check enforces in CI, checked here as a unit test.
//
//nolint:paralleltest // t.Chdir is incompatible with t.Parallel
func TestDocgenYAMLByteIdentical(t *testing.T) {
	chdirRepoRoot(t)

	modVersions, err := reflectcfg.ParseGoMod()
	testza.AssertNoError(t, err)

	out := reflectcfg.Reflect(defaultConfigs, modVersions)

	var buf bytes.Buffer
	testza.AssertNoError(t, reflectcfg.EncodeYAML(&buf, out))

	want, err := os.ReadFile("docs.yaml")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, string(want), buf.String())
}

// TestSchemaByteIdentical asserts the checked-in lakta.schema.json matches a
// fresh regeneration from the built-ins — the anti-drift guard schema-check
// enforces in CI, checked here as a unit test.
//
//nolint:paralleltest // t.Chdir is incompatible with t.Parallel
func TestSchemaByteIdentical(t *testing.T) {
	chdirRepoRoot(t)

	modVersions, err := reflectcfg.ParseGoMod()
	testza.AssertNoError(t, err)

	out := reflectcfg.Reflect(defaultConfigs, modVersions)

	var buf bytes.Buffer
	testza.AssertNoError(t, reflectcfg.EncodeSchema(&buf, out, schemaID))

	want, err := os.ReadFile("lakta.schema.json")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, string(want), buf.String())
}
