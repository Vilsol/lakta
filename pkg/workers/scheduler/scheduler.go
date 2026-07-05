package scheduler

import (
	"context"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Vilsol/slox"
	"github.com/go-co-op/gocron/v2" // verified v2.21.2
	"github.com/samber/oops"
	"github.com/sourcegraph/conc/panics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// JobInfo is the flat, gocron-free introspection record the actuator reads via
// [Scheduler.Jobs]. No gocron types leak.
type JobInfo struct {
	Name     string
	Schedule string
	Timezone string
	Enabled  bool
	NextRun  time.Time
	LastRun  time.Time
}

// Scheduler wraps a gocron.Scheduler plus a name→job map, guarded by mu for
// concurrent Register/RunNow during hot-reload.
type Scheduler struct {
	cron   gocron.Scheduler // verified v2.21.2: Scheduler is an interface
	tracer oteltrace.Tracer // module tracer (noop when otel absent)
	defTZ  string           // scheduler-wide default timezone
	runCtx context.Context  //nolint:containedctx // parent ctx for gocron.WithContext; carries values, gocron owns cancellation

	mu    sync.Mutex
	jobs  map[string]gocron.Job // verified v2.21.2: gocron.Job is an interface
	specs map[string]JobSpec    // recorded spec per registered name (for Jobs()/reload diff)
}

// Register builds (or replaces) the gocron job for name from spec. A disabled
// spec (Enabled != nil && !*Enabled) or one with a nil Handler removes any
// existing job and returns nil without registering. Wraps spec.Handler via wrap
// before handing it to gocron.
func (s *Scheduler) Register(name string, spec JobSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !spec.enabled() || spec.Handler == nil {
		return s.removeLocked(name)
	}

	def, err := spec.jobDefinition(s.defTZ)
	if err != nil {
		return oops.Wrapf(err, "invalid schedule for job %q", name)
	}

	if err := s.removeLocked(name); err != nil {
		return err
	}

	opts := spec.jobOptions(name)
	opts = append(opts, gocron.WithContext(s.runCtx)) // verified v2.21.2: injects per-run ctx, cancelled on Shutdown

	job, err := s.cron.NewJob(def, gocron.NewTask(s.wrap(name, spec.Jitter, spec.Handler)), opts...)
	if err != nil {
		return oops.Wrapf(err, "failed to register job %q", name)
	}

	s.jobs[name] = job
	s.specs[name] = spec

	return nil
}

// removeLocked removes a registered job by its stored gocron ID. Caller holds mu.
func (s *Scheduler) removeLocked(name string) error {
	job, ok := s.jobs[name]
	if !ok {
		return nil
	}

	if err := s.cron.RemoveJob(job.ID()); err != nil { // verified v2.21.2: RemoveJob keys on uuid.UUID
		return oops.Wrapf(err, "failed to remove job %q", name)
	}

	delete(s.jobs, name)
	delete(s.specs, name)

	return nil
}

// remove removes a registered job under mu.
func (s *Scheduler) remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.removeLocked(name)
}

// RunNow fires the named job once, out of schedule. Unknown name returns an
// error listing the known job names.
func (s *Scheduler) RunNow(name string) error {
	job, err := s.lookup(name)
	if err != nil {
		return err
	}

	return oops.Wrapf(job.RunNow(), "failed to run job %q", name) // verified v2.21.2: Job.RunNow() error
}

// NextRun returns the next scheduled fire for name. Unknown name errors as above.
func (s *Scheduler) NextRun(name string) (time.Time, error) {
	job, err := s.lookup(name)
	if err != nil {
		return time.Time{}, err
	}

	next, err := job.NextRun() // verified v2.21.2: Job.NextRun() (time.Time, error)

	return next, oops.Wrapf(err, "failed to get next run for job %q", name)
}

// lookup returns the named live job, or an error listing the known names.
func (s *Scheduler) lookup(name string) (gocron.Job, error) { //nolint:ireturn // gocron.Job is the library interface
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[name]
	if !ok {
		known := slices.Sorted(maps.Keys(s.jobs))
		return nil, oops.Errorf("unknown scheduled job %q (known jobs: %v)", name, known)
	}

	return job, nil
}

// Jobs snapshots all live jobs as JobInfo for introspection, sorted by name.
func (s *Scheduler) Jobs() []JobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	infos := make([]JobInfo, 0, len(s.jobs))
	for name, job := range s.jobs {
		spec := s.specs[name]

		tz := spec.Timezone
		if tz == "" {
			tz = s.defTZ
		}

		next, _ := job.NextRun()
		last, _ := job.LastRunStartedAt() // verified v2.21.2: LastRun() is deprecated

		infos = append(infos, JobInfo{
			Name:     name,
			Schedule: spec.Schedule,
			Timezone: tz,
			Enabled:  spec.enabled(),
			NextRun:  next,
			LastRun:  last,
		})
	}

	slices.SortFunc(infos, func(a, b JobInfo) int { return strings.Compare(a.Name, b.Name) })

	return infos
}

// wrap decorates a handler with jitter, an otel span, panic recovery and slog
// logging — the scheduler analogue of pool.execute. The per-run ctx is injected
// by gocron (job registered with gocron.WithContext) and cancelled on Shutdown.
func (s *Scheduler) wrap(name string, jitter time.Duration, fn func(ctx context.Context) error) func(context.Context) {
	return func(ctx context.Context) {
		if jitter > 0 {
			time.Sleep(rand.N(jitter)) //nolint:gosec // jitter spreads load, not security-sensitive
		}

		ctx, span := s.tracer.Start(ctx, "scheduler.job",
			oteltrace.WithAttributes(attribute.String("job.name", name)))
		defer span.End()

		var err error
		recovered := panics.Try(func() { err = fn(ctx) })

		switch {
		case recovered != nil:
			span.SetStatus(codes.Error, "panic")
			slox.Error(ctx, "scheduled job panicked",
				slog.String("job", name), slog.String("panic", recovered.String()))
		case err != nil:
			span.SetStatus(codes.Error, err.Error())
			slox.Error(ctx, "scheduled job failed",
				slog.String("job", name), slog.Any("error", err))
		}
	}
}
