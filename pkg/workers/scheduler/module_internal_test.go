package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

const (
	everyFiveMin = "@every 5m"
	everyHour    = "@every 1h"
)

func TestNewConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := NewConfig()

	testza.AssertEqual(t, "default", cfg.Name)
	testza.AssertEqual(t, "UTC", cfg.Timezone)
	testza.AssertEqual(t, 0, len(cfg.Jobs))
}

func TestLoadConfig_ReadsJobDefinitions(t *testing.T) {
	t.Parallel()
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		"modules.workers.scheduler.default.timezone":             "Europe/Riga",
		"modules.workers.scheduler.default.jobs.report.schedule": everyFiveMin,
		"modules.workers.scheduler.default.jobs.report.overlap":  "queue",
		"modules.workers.scheduler.default.jobs.report.jitter":   "3s",
	}, "."), nil))

	m := NewModule()
	testza.AssertNoError(t, m.LoadConfig(k))

	testza.AssertEqual(t, "Europe/Riga", m.config.Timezone)
	spec, ok := m.config.Jobs["report"]
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, everyFiveMin, spec.Schedule)
	testza.AssertEqual(t, OverlapQueue, spec.Overlap)
	testza.AssertEqual(t, 3*time.Second, spec.Jitter)
}

func TestMergedJobs_ConfigOverlaysCodeButKeepsHandler(t *testing.T) {
	t.Parallel()
	called := false
	m := NewModule(WithJob("cleanup", everyHour, func(context.Context) error {
		called = true
		return nil
	}))

	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		"modules.workers.scheduler.default.jobs.cleanup.schedule": everyFiveMin,
		"modules.workers.scheduler.default.jobs.cleanup.enabled":  false,
	}, "."), nil))
	testza.AssertNoError(t, m.LoadConfig(k))

	spec := m.config.MergedJobs()["cleanup"]
	testza.AssertEqual(t, everyFiveMin, spec.Schedule) // config schedule wins
	testza.AssertFalse(t, spec.enabled())              // config disabled it
	testza.AssertNotNil(t, spec.Handler)               // code handler persists

	testza.AssertNoError(t, spec.Handler(context.Background()))
	testza.AssertTrue(t, called)
}

func TestJobDefinition_EveryAndCron(t *testing.T) {
	t.Parallel()

	_, err := JobSpec{Schedule: everyFiveMin}.jobDefinition("UTC")
	testza.AssertNoError(t, err)

	_, err = JobSpec{Schedule: "@every nonsense"}.jobDefinition("UTC")
	testza.AssertNotNil(t, err)

	_, err = JobSpec{Schedule: "0 0 12 * * *", Timezone: "America/New_York"}.jobDefinition("UTC")
	testza.AssertNoError(t, err)
}

func TestSpecChanged(t *testing.T) {
	t.Parallel()
	base := JobSpec{Schedule: everyHour, Overlap: OverlapSkip}

	testza.AssertFalse(t, specChanged(base, JobSpec{Schedule: everyHour, Overlap: OverlapSkip}))
	testza.AssertTrue(t, specChanged(base, JobSpec{Schedule: "@every 2h", Overlap: OverlapSkip}))

	disabled := false
	testza.AssertTrue(t, specChanged(base, JobSpec{Schedule: everyHour, Overlap: OverlapSkip, Enabled: &disabled}))
}
