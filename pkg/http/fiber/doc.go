// Package fiberserver provides a lakta module for HTTP servers using Fiber.
//
// Default timeouts are applied when not set: ReadTimeout 30s, WriteTimeout 60s,
// IdleTimeout 120s. Override them via WithDefaults(fiber.Config{...}) or the
// koanf Raw passthrough. Streaming or long-lived endpoints may need a larger
// or zero WriteTimeout.
package fiberserver
