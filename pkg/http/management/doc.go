// Package management provides the management HTTP server [Module]: liveness
// (/health), readiness (/ready, aggregating the *health.Health checks other
// modules register) and Prometheus metrics (/metrics, from the otel module's
// private registry), on a port separate from the API.
package management
