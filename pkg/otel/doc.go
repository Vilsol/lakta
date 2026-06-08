// Package otel provides a lakta module for OpenTelemetry instrumentation.
//
// Telemetry setup is fail-open by default: if SDK initialization fails the app
// logs a warning and continues with noop providers. Set WithRequired(true) (or
// koanf required: true) to make setup failures fatal.
package otel
