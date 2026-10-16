package lakta_test

import (
	"context"
	"io"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

const describePath = "x.y"

type describeCfg struct {
	Port int `koanf:"port"`
}

type describedModule struct {
	path       string
	initCalls  int
	startCalls int
	loadCalls  int
}

func (m *describedModule) Init(context.Context) error     { m.initCalls++; return nil }
func (m *describedModule) Start(context.Context) error    { m.startCalls++; return nil }
func (m *describedModule) Shutdown(context.Context) error { return nil }
func (m *describedModule) ConfigPath() string             { return m.path }
func (m *describedModule) LoadConfig(*koanf.Koanf) error  { m.loadCalls++; return nil }

func (m *describedModule) DescribeConfig(k *koanf.Koanf) (any, error) {
	cfg := describeCfg{Port: 8080}
	if err := k.Unmarshal(m.path, &cfg); err != nil {
		return nil, err //nolint:wrapcheck // test fake
	}
	return cfg, nil
}

type describeLoaderModule struct {
	values map[string]any
}

func (m *describeLoaderModule) Init(context.Context) error     { return nil }
func (m *describeLoaderModule) Shutdown(context.Context) error { return nil }

func (m *describeLoaderModule) LoadForDescribe() (*koanf.Koanf, error) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(m.values, "."), nil); err != nil {
		return nil, err //nolint:wrapcheck // test fake
	}
	return k, nil
}

func TestDescribe_ReportsConfigWithoutInitOrStart(t *testing.T) {
	t.Setenv("LAKTA_DESCRIBE", "config")

	mod := &describedModule{path: "modules.test.thing.default"}
	loader := &describeLoaderModule{values: map[string]any{"modules.test.thing.default.port": 9090}}

	var got []lakta.DescribedConfig
	err := lakta.NewRuntime(loader, mod).
		WithDescriber(func(_ io.Writer, entries []lakta.DescribedConfig) error {
			got = entries
			return nil
		}).
		RunContext(t.Context())

	testza.AssertNil(t, err)
	testza.AssertEqual(t, []lakta.DescribedConfig{{Path: "modules.test.thing.default", Value: describeCfg{Port: 9090}}}, got)
	testza.AssertEqual(t, 0, mod.initCalls)
	testza.AssertEqual(t, 0, mod.startCalls)
	testza.AssertEqual(t, 0, mod.loadCalls)
}

func TestDescribe_WithoutLoaderUsesDefaults(t *testing.T) {
	t.Setenv("LAKTA_DESCRIBE", "config")

	var got []lakta.DescribedConfig
	err := lakta.NewRuntime(&describedModule{path: describePath}).
		WithDescriber(func(_ io.Writer, entries []lakta.DescribedConfig) error {
			got = entries
			return nil
		}).
		RunContext(t.Context())

	testza.AssertNil(t, err)
	testza.AssertEqual(t, []lakta.DescribedConfig{{Path: describePath, Value: describeCfg{Port: 8080}}}, got)
}

func TestDescribe_WithoutDescriberFails(t *testing.T) {
	t.Setenv("LAKTA_DESCRIBE", "config")

	err := lakta.NewRuntime(&describedModule{path: describePath}).RunContext(t.Context())
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "describe mode needs Runtime.WithDescriber")
}

func TestDescribe_UnknownModeFails(t *testing.T) {
	t.Setenv("LAKTA_DESCRIBE", "everything")

	err := lakta.NewRuntime(&describedModule{path: describePath}).
		WithDescriber(func(io.Writer, []lakta.DescribedConfig) error { return nil }).
		RunContext(t.Context())
	testza.AssertNotNil(t, err)
}
