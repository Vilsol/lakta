package pool

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

func TestLoadConfig_ReadsPoolDefinitions(t *testing.T) {
	t.Parallel()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		"modules.workers.pool.default.pools.emails.workers":    4,
		"modules.workers.pool.default.pools.emails.queue_size": 256,
	}, "."), nil))

	m := NewModule()
	testza.AssertNoError(t, m.LoadConfig(k))

	cfg, ok := m.config.Pools["emails"]
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, 4, cfg.Workers)
	testza.AssertEqual(t, 256, *cfg.QueueSize)
}

func TestNewConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := NewConfig()

	testza.AssertEqual(t, "default", cfg.Name)
	testza.AssertEqual(t, 0, len(cfg.Pools))
}
