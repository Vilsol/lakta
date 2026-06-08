package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"
)

func writeReloadFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReload_ValidatorVetoKeepsOldConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeReloadFile(t, path, "foo: original\n")

	m := NewModule()
	m.configFiles = []configFile{{path: path, parser: yaml.Parser()}}
	testza.AssertNoError(t, m.reload())
	testza.AssertEqual(t, "original", m.Koanf().String("foo"))

	writeReloadFile(t, path, "foo: changed\n")
	m.OnValidate(func(*koanf.Koanf) error { return errors.New("veto") })

	err := m.reload()
	testza.AssertNotNil(t, err)
	testza.AssertEqual(t, "original", m.Koanf().String("foo")) // unchanged after veto
}

func TestReload_CallbackPanicIsolated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeReloadFile(t, path, "foo: v1\n")

	m := NewModule()
	m.configFiles = []configFile{{path: path, parser: yaml.Parser()}}
	testza.AssertNoError(t, m.reload())

	ran := false
	m.OnReload(func(*koanf.Koanf) { panic("callback boom") })
	m.OnReload(func(*koanf.Koanf) { ran = true })

	writeReloadFile(t, path, "foo: v2\n")
	testza.AssertNoError(t, m.reload())
	testza.AssertTrue(t, ran, "later callback must still run after an earlier one panics")
}
