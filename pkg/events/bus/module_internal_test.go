package bus

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func TestLoadConfig_ReadsBufferSize(t *testing.T) {
	t.Parallel()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		"modules.events.bus.default.buffer_size": 7,
	}, "."), nil))

	m := NewModule()
	testza.AssertNoError(t, m.LoadConfig(k))

	testza.AssertEqual(t, 7, m.config.BufferSize)
}

func TestNewConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := NewConfig()

	testza.AssertEqual(t, "default", cfg.Name)
	testza.AssertEqual(t, defaultBufferSize, cfg.BufferSize)
}
