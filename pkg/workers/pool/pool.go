package pool

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"

	"github.com/Vilsol/slox"
	"github.com/samber/oops"
	"github.com/sourcegraph/conc"
	"github.com/sourcegraph/conc/panics"
)

const defaultQueueSize = 1024

// ErrPoolClosed is returned by Submit after the pool has been closed.
var ErrPoolClosed = errors.New("worker pool is closed")

// PoolConfig configures a single pool.
type PoolConfig struct {
	// Workers is the number of concurrent workers. Zero or less uses NumCPU.
	Workers int `koanf:"workers"`

	// QueueSize is the pending-task queue capacity. Nil uses the default
	// (1024); zero means direct handoff to an idle worker.
	QueueSize *int `koanf:"queue_size"`
}

type task struct {
	ctx context.Context //nolint:containedctx // carries the submitter's context per task
	run func(ctx context.Context) error
}

// Pool executes submitted tasks on a bounded set of workers fed by a bounded
// FIFO queue.
type Pool struct {
	queue    chan task
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	mu     sync.RWMutex
	closed bool
}

// New creates a pool and starts its workers.
func New(cfg PoolConfig) *Pool {
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	queueSize := defaultQueueSize
	if cfg.QueueSize != nil {
		queueSize = *cfg.QueueSize
	}

	p := &Pool{
		queue: make(chan task, queueSize),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}

	var wg conc.WaitGroup
	for range workers {
		wg.Go(p.work)
	}
	go func() {
		defer close(p.done)
		wg.Wait()
	}()

	return p
}

// Submit enqueues a fire-and-forget task, blocking while the queue is full
// until ctx is done. The task context is detached from the submitter's
// cancellation but keeps its values. Task errors are logged, panics recovered.
func (p *Pool) Submit(ctx context.Context, fn func(ctx context.Context) error) error {
	return p.enqueue(ctx, task{
		ctx: context.WithoutCancel(ctx),
		run: fn,
	})
}

func (p *Pool) enqueue(ctx context.Context, t task) error {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return ErrPoolClosed
	}

	select {
	case p.queue <- t:
		return nil
	case <-p.stop:
		return ErrPoolClosed
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "failed to enqueue task")
	}
}

// Close stops intake and drains queued and in-flight tasks, bounded by ctx.
// Submit returns [ErrPoolClosed] afterwards. Close is idempotent.
func (p *Pool) Close(ctx context.Context) error {
	p.beginClose()
	return p.awaitClose(ctx)
}

func (p *Pool) beginClose() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.stopOnce.Do(func() { close(p.stop) })
}

func (p *Pool) awaitClose(ctx context.Context) error {
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "timed out draining worker pool")
	}
}

// work consumes the queue until stopped, then drains whatever is left.
func (p *Pool) work() {
	for {
		select {
		case t := <-p.queue:
			p.execute(t)
		case <-p.stop:
			for {
				select {
				case t := <-p.queue:
					p.execute(t)
				default:
					return
				}
			}
		}
	}
}

func (p *Pool) execute(t task) {
	var err error
	recovered := panics.Try(func() {
		err = t.run(t.ctx)
	})

	switch {
	case recovered != nil:
		slox.Error(t.ctx, "worker task panicked", slog.String("panic", recovered.String()))
	case err != nil:
		slox.Error(t.ctx, "worker task failed", slog.Any("error", err))
	}
}
