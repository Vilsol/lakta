// Package scheduler provides a config-driven cron/interval job scheduler as a
// lakta AsyncModule, wrapping go-co-op/gocron v2 with code-owned handlers,
// otel spans, panic recovery, overlap policies, per-job timezone/jitter and
// hot-reload.
package scheduler
