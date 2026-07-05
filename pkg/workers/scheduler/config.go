package scheduler

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/go-co-op/gocron/v2" // verified v2.21.2
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// OverlapPolicy controls what happens when a job's previous run is still in
// flight at its next fire. Maps to a gocron singleton mode.
type OverlapPolicy string

const (
	OverlapSkip  OverlapPolicy = "skip"  // drop the overlapping run (LimitModeReschedule)
	OverlapQueue OverlapPolicy = "queue" // serialize: run after the current one (LimitModeWait)
	OverlapAllow OverlapPolicy = "allow" // no singleton option; runs may overlap
)

// everyPrefix marks an interval schedule handled by gocron.DurationJob.
const everyPrefix = "@every"

// JobSpec declares one scheduled job. Handler is code-owned (never from YAML);
// every other field is config-overridable by job name.
type JobSpec struct {
	Schedule string        `koanf:"schedule"` // 6-field cron (seconds) or "@every 5m"
	Timezone string        `koanf:"timezone"` // per-job override of Config.Timezone; "" inherits
	Jitter   time.Duration `koanf:"jitter"`   // 0 = none
	Overlap  OverlapPolicy `koanf:"overlap"`  // "" defaults to OverlapSkip in translation

	// Enabled uses nil = true; false = never registered. This is the OPPOSITE
	// convention to the actuator's enabled:false default — here a job is on
	// unless a config entry explicitly disables it.
	Enabled *bool `koanf:"enabled"`

	// Handler runs per fire. koanf:"-" keeps config from ever carrying a func;
	// it survives hot-reload because config only overlays the other fields.
	Handler func(ctx context.Context) error `koanf:"-"`
}

// Config is the worker-scheduler module config. Mirrors pool.Config: a config
// map (Jobs) overlays a code-only map (CodeJobs) by name via MergedJobs.
type Config struct {
	// Instance name; DefaultInstanceName by default.
	Name string `koanf:"-"`

	// Timezone is the scheduler-wide default location (IANA name). Per-job
	// JobSpec.Timezone overrides it. Defaults to "UTC".
	Timezone string `koanf:"timezone"`

	// Jobs holds config-declared job overlays. Prefer snake_case names;
	// hyphens cannot be overridden via environment variables.
	Jobs map[string]JobSpec `koanf:"jobs"`

	// CodeJobs holds jobs registered via WithJob (code-only). A config entry
	// with the same name overlays it field-by-field (config wins per field);
	// the Handler always persists.
	CodeJobs map[string]JobSpec `code_only:"WithJob" koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name:     config.DefaultInstanceName,
		Timezone: "UTC",
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return config.Apply(NewDefaultConfig(), options...)
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// MergedJobs copies CodeJobs then overlays Jobs field-by-field per name; config
// wins per field while the code-owned Handler always persists (config never
// carries a func).
func (c *Config) MergedJobs() map[string]JobSpec {
	merged := make(map[string]JobSpec, len(c.CodeJobs)+len(c.Jobs))
	maps.Copy(merged, c.CodeJobs)
	for name, override := range c.Jobs {
		merged[name] = merged[name].overlaidWith(override)
	}
	return merged
}

// overlaidWith returns s with the non-zero fields of o applied, preserving
// s.Handler (config never carries a func).
func (s JobSpec) overlaidWith(o JobSpec) JobSpec {
	if o.Schedule != "" {
		s.Schedule = o.Schedule
	}
	if o.Timezone != "" {
		s.Timezone = o.Timezone
	}
	if o.Jitter != 0 {
		s.Jitter = o.Jitter
	}
	if o.Overlap != "" {
		s.Overlap = o.Overlap
	}
	if o.Enabled != nil {
		s.Enabled = o.Enabled
	}
	return s
}

// enabled reports whether the job should be registered (nil = true).
func (s JobSpec) enabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithTimezone sets the scheduler-wide default location (code-only; the config
// timezone key still wins on load).
func WithTimezone(tz string) Option {
	return func(m *Config) { m.Timezone = tz }
}

// WithJob registers a code-owned job. Seeds CodeJobs[name] with Schedule +
// Handler; the remaining JobSpec fields keep their zero defaults and config may
// override them by the same name.
func WithJob(name, schedule string, fn func(ctx context.Context) error) Option {
	return func(m *Config) {
		if m.CodeJobs == nil {
			m.CodeJobs = make(map[string]JobSpec)
		}
		m.CodeJobs[name] = JobSpec{Schedule: schedule, Handler: fn}
	}
}

// gocronMode translates the overlap policy to the singleton job option. The
// bool is false for OverlapAllow (no option). An empty policy defaults to skip.
func (p OverlapPolicy) gocronMode() (gocron.JobOption, bool) {
	switch p {
	case OverlapAllow:
		return nil, false
	case OverlapQueue:
		return gocron.WithSingletonMode(gocron.LimitModeWait), true // verified v2.21.2
	case OverlapSkip:
		return gocron.WithSingletonMode(gocron.LimitModeReschedule), true // verified v2.21.2
	default:
		return gocron.WithSingletonMode(gocron.LimitModeReschedule), true
	}
}

// jobDefinition builds the gocron definition from Schedule: a DurationJob for
// "@every ..." intervals, otherwise a 6-field CronJob (with seconds). Per-job
// timezone is encoded on cron schedules via robfig's CRON_TZ token, since
// gocron has no per-job location option; DurationJob has no wall-clock TZ so
// Timezone is a no-op there.
func (s JobSpec) jobDefinition(defaultTZ string) (gocron.JobDefinition, error) { //nolint:ireturn // gocron.JobDefinition is the library interface
	if raw, ok := strings.CutPrefix(s.Schedule, everyPrefix); ok {
		d, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return nil, oops.Wrapf(err, "invalid %q interval %q", everyPrefix, s.Schedule)
		}
		return gocron.DurationJob(d), nil
	}

	tz := s.Timezone
	if tz == "" {
		tz = defaultTZ
	}
	crontab := s.Schedule
	if tz != "" {
		crontab = "CRON_TZ=" + tz + " " + s.Schedule
	}
	return gocron.CronJob(crontab, true), nil // verified v2.21.2: CronJob(crontab, withSeconds)
}

// jobOptions assembles per-job options: gocron.WithName(name) plus the overlap
// singleton mode. Jitter is applied in the wrap wrapper (gocron v2 has no
// jitter option), so no jitter option is emitted here.
func (s JobSpec) jobOptions(name string) []gocron.JobOption {
	opts := []gocron.JobOption{gocron.WithName(name)} // verified v2.21.2
	if mode, ok := s.Overlap.gocronMode(); ok {
		opts = append(opts, mode)
	}
	return opts
}
