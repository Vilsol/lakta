package actuator

import (
	"context"

	"github.com/Vilsol/slox"
	"github.com/gofiber/fiber/v3"
	"github.com/samber/oops"
)

// maxProfileSeconds caps pprof CPU/trace profile durations to close the
// unbounded-?seconds= CPU-DoS vector. Not specified by the design; 30s chosen.
const maxProfileSeconds = 30

// applySecurity enforces the fail-closed model during Init. With auth
// configured, nothing is refused. Without auth on a loopback bind the module
// still starts, but sensitive endpoints reject every request (see
// authMiddleware). Without auth on a non-loopback bind the module refuses to
// start unless AllowInsecure downgrades the refusal to a warning.
func (m *Module) applySecurity(ctx context.Context) error {
	if m.config.Auth != nil {
		return nil
	}

	if m.config.isLoopback() {
		slox.Warn(ctx, "actuator: no auth configured; sensitive endpoints (goroutine/pprof/vars/loggers) will reject all requests")
		return nil
	}

	msg := "actuator bound to non-loopback address without auth"
	if m.config.AllowInsecure {
		slox.Warn(ctx, msg+" (allow_insecure=true)")
		return nil
	}

	return oops.
		With("host", m.config.Host).
		Errorf("%s: provide WithAuth or set allow_insecure=true", msg)
}

// authMiddleware returns the middleware gating sensitive endpoints. When no auth
// is configured it fails closed, rejecting every request with 401 (loopback is
// false comfort in containers). AllowInsecure never relaxes this for the
// sensitive routes — it only affects the non-loopback start refusal.
func (m *Module) authMiddleware() fiber.Handler {
	if m.config.Auth != nil {
		return m.config.Auth
	}

	return func(_ fiber.Ctx) error {
		return fiber.NewError(fiber.StatusUnauthorized, "actuator: authentication required")
	}
}

// clampProfileSeconds bounds a requested pprof profile duration to
// [1, maxProfileSeconds].
func clampProfileSeconds(requested int) int {
	if requested < 1 {
		return 1
	}
	if requested > maxProfileSeconds {
		return maxProfileSeconds
	}
	return requested
}
