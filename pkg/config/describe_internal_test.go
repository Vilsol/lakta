package config

import (
	"errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
)

var (
	_ lakta.DescribeLoader  = (*Module)(nil)
	_ lakta.ConfigDescriber = (*bindModule[struct{}])(nil)
)

func TestLoadForDescribe_BaseFilesOnly(t *testing.T) {
	t.Setenv("LAKTA_SHARED", "from-env")

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")
	writeFile(t, dir, "lakta.prod.yaml", "shared: prod\n")

	m := NewModule(WithConfigDirs(dir), WithProfile("prod"), WithArgs([]string{"--shared=flag"}))
	k, err := m.LoadForDescribe()

	testza.AssertNil(t, err)
	testza.AssertEqual(t, "base", k.String("shared"))
}

func TestLoadForDescribe_AppliesCodeValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "lakta.yaml", "shared: base\n")

	m := NewModule(WithConfigDirs(dir), WithValues(map[string]any{"shared": "code"}))
	k, err := m.LoadForDescribe()

	testza.AssertNil(t, err)
	testza.AssertEqual(t, "code", k.String("shared"))
}

func TestDescribeConfigDirs_DropsSystemOverlay(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, []string{"a", "b"}, describeConfigDirs([]string{"a", "b", "/etc/lakta"}))
}

type strictCfg struct {
	Name string `koanf:"name"`
}

func (c *strictCfg) Validate() error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func TestBindDescribe_SkipsValidation(t *testing.T) {
	t.Parallel()

	m := Bind[strictCfg]("app", "svc")
	value, err := m.DescribeConfig(koanf.New("."))

	testza.AssertNil(t, err)
	testza.AssertEqual(t, &strictCfg{}, value)
}

func TestBindDescribe_ReadsValues(t *testing.T) {
	t.Parallel()

	k := koanf.New(".")
	testza.AssertNil(t, k.Set("app.svc.name", "x"))

	value, err := Bind[strictCfg]("app", "svc").DescribeConfig(k)

	testza.AssertNil(t, err)
	testza.AssertEqual(t, &strictCfg{Name: "x"}, value)
}
