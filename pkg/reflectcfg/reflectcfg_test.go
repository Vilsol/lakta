package reflectcfg_test

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/reflectcfg"
)

// fakeModule mimics the ConfigPath half of the lakta.Configurable contract
// consumed by FromModule.
type fakeModule struct{ path string }

func (m fakeModule) ConfigPath() string { return m.path }

type widgetConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port" required:"true"`
}

func TestReflectFromModule(t *testing.T) {
	t.Parallel()

	entries := []reflectcfg.Entry{
		reflectcfg.FromModule(fakeModule{path: "modules.custom.widget.default"}, widgetConfig{Host: "0.0.0.0", Port: 8080}),
	}

	out := reflectcfg.Reflect(entries, nil)

	testza.AssertEqual(t, 1, len(out.Modules))
	m := out.Modules[0]
	// category/type come from the module's declared ConfigPath, not the
	// (non-lakta) package path; the instance segment becomes <name>.
	testza.AssertEqual(t, "custom", m.Category)
	testza.AssertEqual(t, "widget", m.Type)
	testza.AssertEqual(t, "modules.custom.widget.<name>", m.ConfigPath)
	testza.AssertEqual(t, 2, len(m.Fields))
	testza.AssertEqual(t, "LAKTA_MODULES__CUSTOM__WIDGET__<NAME>__PORT", m.Fields[1].EnvVar)
	testza.AssertEqual(t, "8080", m.Fields[1].Default)
	testza.AssertTrue(t, m.Fields[1].Required)
}

func TestReflectPointerConfig(t *testing.T) {
	t.Parallel()

	out := reflectcfg.Reflect([]reflectcfg.Entry{
		{Path: "modules.custom.widget.default", Config: &widgetConfig{Port: 9090}},
	}, nil)

	testza.AssertEqual(t, 1, len(out.Modules))
	testza.AssertEqual(t, "custom", out.Modules[0].Category)
	testza.AssertEqual(t, "9090", out.Modules[0].Fields[1].Default)
}
