package fiberserver

import (
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/gofiber/fiber/v3"
)

var (
	_ lakta.SyncModule   = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

func TestToFiberConfig_AppliesGenerousTimeoutDefaults(t *testing.T) {
	t.Parallel()

	c := NewConfig()
	cfg := c.ToFiberConfig()

	testza.AssertEqual(t, 30*time.Second, cfg.ReadTimeout)
	testza.AssertEqual(t, 60*time.Second, cfg.WriteTimeout)
	testza.AssertEqual(t, 120*time.Second, cfg.IdleTimeout)
}

func TestToFiberConfig_UserDefaultsOverrideTimeouts(t *testing.T) {
	t.Parallel()

	c := NewConfig(WithDefaults(fiber.Config{ReadTimeout: 5 * time.Second}))
	cfg := c.ToFiberConfig()

	testza.AssertEqual(t, 5*time.Second, cfg.ReadTimeout)   // user value preserved
	testza.AssertEqual(t, 60*time.Second, cfg.WriteTimeout) // unset → default
}
