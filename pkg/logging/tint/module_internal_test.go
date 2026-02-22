package tint

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/knadh/koanf/v2"
)

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Provider     = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

func TestTintModule_LoadsTimeFormatFromKoanf(t *testing.T) {
	t.Parallel()

	k := koanf.New(".")
	testza.AssertNil(t, k.Load(testkit.MapProvider(map[string]any{
		"modules": map[string]any{
			"logging": map[string]any{
				"tint": map[string]any{
					"default": map[string]any{
						"time_format": "15:04:05",
					},
				},
			},
		},
	}), nil))

	m := NewModule()
	testza.AssertNil(t, m.LoadConfig(k))
	testza.AssertEqual(t, "15:04:05", m.config.TimeFormat)
}

func TestWithTimeFormat_SetsConfig(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(WithTimeFormat("15:04"))
	testza.AssertEqual(t, "15:04", cfg.TimeFormat)
}

func TestNewDefaultConfig_Defaults(t *testing.T) {
	t.Parallel()

	cfg := NewDefaultConfig()
	testza.AssertNotNil(t, cfg.Writer)
	testza.AssertNotEqual(t, "", cfg.TimeFormat)
}

func TestTintOptions_Level(t *testing.T) {
	t.Parallel()

	cfg := NewDefaultConfig()
	opts := cfg.TintOptions()
	testza.AssertTrue(t, opts.AddSource)
}
