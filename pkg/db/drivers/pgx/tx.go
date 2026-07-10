// Declarative transactions for the pgx module.
//
// WARNING: pgx.Tx is NOT safe to share across goroutines. The active tx is
// stashed in ctx wrapped in a *txState carrying a done flag. Q(ctx) hands out
// that tx only while it is live; once the owning WithTx has committed or rolled
// back, done is set and Q falls back to the pool. This is load-bearing: a task
// detached inside WithTx via workers/pool.Submit copies the tx ctx value under
// context.WithoutCancel (values kept, cancellation dropped), so without the
// guard it could touch a finished tx. Q in a spawned goroutine returning the
// pool — never the tx — is by design, not a bug. Do not "fix" it.

package pgx

import (
	"context"
	"sync/atomic"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// Querier is the subset shared by *pgxpool.Pool and pgx.Tx. Repos call Q(ctx)
// and get one of the two transparently. Verified: both satisfy this set.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

var (
	_ Querier = (*pgxpool.Pool)(nil)
	_ Querier = (pgx.Tx)(nil)
)

// txCtxKey is instance-scoped so a Q for "default" never picks up another
// instance's transaction.
type txCtxKey struct{ instance string }

// txState wraps the active tx with a done flag. done is set true in every
// terminal path (commit, rollback, panic) BEFORE the terminal call returns, so
// a detached goroutine that copied the ctx value sees the tx as finished.
type txState struct {
	tx   pgx.Tx
	done atomic.Bool
}

func (s *txState) markDone()  { s.done.Store(true) }
func (s *txState) live() bool { return !s.done.Load() }

// Q returns the active transaction when one is live in ctx, else the pool for
// the default instance. It requires a runtime/harness ctx carrying the lakta
// injector; a failed pool lookup yields an errQuerier that fails loudly at
// first use rather than nil-panicking.
func Q(ctx context.Context) Querier { //nolint:ireturn // Querier is the shared pool/tx interface
	return resolveQuerier(ctx, config.DefaultInstanceName, nil)
}

// QNamed is the free-function variant of Q for a named instance; it resolves
// the pool via DI.
func QNamed(ctx context.Context, name string) Querier { //nolint:ireturn // Querier is the shared pool/tx interface
	return resolveQuerier(ctx, name, nil)
}

// Querier is the named-instance equivalent of Q, resolving against
// m.config.Name and falling back to the pool this module owns.
func (m *Module) Querier(ctx context.Context) Querier { //nolint:ireturn // Querier is the shared pool/tx interface
	return resolveQuerier(ctx, m.config.Name, m.instance)
}

// resolveQuerier is the shared Q logic: a live tx under the instance key wins,
// otherwise the fallback pool (an explicitly-held pool, else the DI pool).
func resolveQuerier(ctx context.Context, instance string, fallback *pgxpool.Pool) Querier { //nolint:ireturn // Querier is the shared pool/tx interface
	if st, ok := ctx.Value(txCtxKey{instance: instance}).(*txState); ok && st.live() {
		return st.tx
	}
	if fallback != nil {
		return fallback
	}
	pool, err := lakta.Invoke[*pgxpool.Pool](ctx)
	if err != nil {
		return errQuerier{err: oops.Wrapf(err, "no active transaction and no *pgxpool.Pool in context")}
	}
	return pool
}

// TxOption configures a top-level transaction's pgx.TxOptions.
type TxOption func(*pgx.TxOptions)

// WithIsolation sets the transaction isolation level.
func WithIsolation(level pgx.TxIsoLevel) TxOption {
	return func(o *pgx.TxOptions) { o.IsoLevel = level }
}

// WithReadOnly starts the transaction in read-only access mode.
func WithReadOnly() TxOption {
	return func(o *pgx.TxOptions) { o.AccessMode = pgx.ReadOnly }
}

// WithDeferrable starts the transaction in deferrable mode.
func WithDeferrable() TxOption {
	return func(o *pgx.TxOptions) { o.DeferrableMode = pgx.Deferrable }
}

func buildTxOptions(opts []TxOption) pgx.TxOptions {
	var o pgx.TxOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithTx runs fn inside a transaction on the default instance. If a live tx is
// already in ctx, fn runs in a nested SAVEPOINT (opts ignored — pgx savepoints
// inherit the parent isolation; a debug log notes any isolation opts passed).
// It commits on success, rolls back on error, and rolls back and re-panics on
// panic, setting done in every terminal path.
func WithTx(ctx context.Context, fn func(context.Context) error, opts ...TxOption) error {
	key := txCtxKey{instance: config.DefaultInstanceName}
	if st, ok := ctx.Value(key).(*txState); ok && st.live() {
		return nestedTx(ctx, key, config.DefaultInstanceName, st, opts, fn)
	}
	return beginTopLevel(ctx, key, config.DefaultInstanceName, opts, fn)
}

// WithNewTx forces a fresh top-level tx on the pool even when one is already in
// ctx (it does not nest); same commit/rollback/guard machinery as WithTx.
func WithNewTx(ctx context.Context, fn func(context.Context) error, opts ...TxOption) error {
	return beginTopLevel(ctx, txCtxKey{instance: config.DefaultInstanceName}, config.DefaultInstanceName, opts, fn)
}

func beginTopLevel(ctx context.Context, key txCtxKey, instance string, opts []TxOption, fn func(context.Context) error) error {
	pool, err := lakta.Invoke[*pgxpool.Pool](ctx)
	if err != nil {
		return oops.Wrapf(err, "failed to resolve *pgxpool.Pool for transaction")
	}

	txOpts := buildTxOptions(opts)
	tx, err := pool.BeginTx(ctx, txOpts)
	if err != nil {
		return oops.Wrapf(err, "failed to begin transaction")
	}

	st := &txState{tx: tx}
	ctx, span := startTxSpan(ctx, instance, false, txOpts)
	ctx = context.WithValue(ctx, key, st)

	return runUnit(ctx, st, span, fn)
}

// nestedTx installs a savepoint as the active tx for the inner scope so further
// nesting savepoints correctly under it; the outer txState is untouched (its
// done stays false — the outer tx survives an inner rollback).
func nestedTx(
	ctx context.Context,
	key txCtxKey,
	instance string,
	parent *txState,
	opts []TxOption,
	fn func(context.Context) error,
) error {
	if len(opts) > 0 {
		slox.Debug(ctx, "WithTx: isolation options ignored for nested (savepoint) transaction")
	}

	sp, err := parent.tx.Begin(ctx) // SAVEPOINT
	if err != nil {
		return oops.Wrapf(err, "failed to create savepoint")
	}

	inner := &txState{tx: sp}
	ctx, span := startTxSpan(ctx, instance, true, pgx.TxOptions{})
	ctx = context.WithValue(ctx, key, inner)

	return runUnit(ctx, inner, span, fn)
}

// runUnit executes fn against st.tx (a top-level tx or a savepoint sub-tx),
// terminating exactly once. done is set unconditionally in commit, rollback,
// and panic paths before returning/re-panicking. For a savepoint, Commit
// releases it and Rollback rolls back to it.
func runUnit(ctx context.Context, st *txState, span oteltrace.Span, fn func(context.Context) error) error {
	defer span.End()

	defer func() {
		if r := recover(); r != nil {
			_ = st.tx.Rollback(ctx)
			st.markDone()
			setOutcome(span, outcomePanic)
			panic(r)
		}
	}()

	if err := fn(ctx); err != nil {
		_ = st.tx.Rollback(ctx)
		st.markDone()
		setOutcome(span, outcomeRollback)
		return err
	}

	if err := st.tx.Commit(ctx); err != nil {
		st.markDone()
		setOutcome(span, outcomeRollback)
		return oops.Wrapf(err, "failed to commit transaction")
	}

	st.markDone()
	setOutcome(span, outcomeCommit)
	return nil
}

const (
	outcomeCommit   = "commit"
	outcomeRollback = "rollback"
	outcomePanic    = "panic"
)

// optionalTracer resolves a Tracer from DI's TracerProvider, or a noop tracer
// when otel is absent — so spans are a no-op without a tracer provider.
func optionalTracer(ctx context.Context) oteltrace.Tracer { //nolint:ireturn // oteltrace.Tracer is the library interface
	if tp, err := lakta.Invoke[oteltrace.TracerProvider](ctx); err == nil {
		return tp.Tracer(meterName)
	}
	return nooptrace.NewTracerProvider().Tracer(meterName)
}

// startTxSpan opens the db.tx span; the caller (runUnit) defers span.End().
//
//nolint:ireturn,spancheck // oteltrace.Span is the library interface; End is deferred by runUnit
func startTxSpan(ctx context.Context, instance string, nested bool, opts pgx.TxOptions) (context.Context, oteltrace.Span) {
	return optionalTracer(ctx).Start(ctx, "db.tx",
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithAttributes(
			attribute.String("db.tx.instance", instance),
			attribute.Bool("db.tx.nested", nested),
			attribute.String("db.tx.isolation", string(opts.IsoLevel)),
			attribute.Bool("db.tx.read_only", opts.AccessMode == pgx.ReadOnly),
		),
	)
}

func setOutcome(span oteltrace.Span, outcome string) {
	span.SetAttributes(attribute.String("db.tx.outcome", outcome))
	if outcome != outcomeCommit {
		span.SetStatus(codes.Error, outcome)
	}
}

// errQuerier is a Querier whose every method returns the stored error, so a
// missing-pool ctx fails loudly at first use instead of nil-panicking.
type errQuerier struct{ err error }

func (e errQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, e.err
}

func (e errQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) { //nolint:ireturn // pgx.Rows is the library interface
	return nil, e.err
}

func (e errQuerier) QueryRow(context.Context, string, ...any) pgx.Row { //nolint:ireturn // pgx.Row is the library interface
	return errRow(e)
}

func (e errQuerier) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, e.err
}

func (e errQuerier) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { //nolint:ireturn // pgx.BatchResults is the library interface
	return errBatchResults(e)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type errBatchResults struct{ err error }

func (b errBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, b.err }

//nolint:ireturn // pgx.Rows is the library interface
func (b errBatchResults) Query() (pgx.Rows, error) { return nil, b.err }

//nolint:ireturn // pgx.Row is the library interface
func (b errBatchResults) QueryRow() pgx.Row { return errRow(b) }

func (b errBatchResults) Close() error { return b.err }
