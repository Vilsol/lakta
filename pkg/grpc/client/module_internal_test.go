package grpcclient

import (
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
)

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
)

func TestClientKeepaliveDefaults(t *testing.T) {
	t.Parallel()

	c := NewConfig()

	kp := c.KeepaliveParams()
	testza.AssertEqual(t, 30*time.Second, kp.Time)
	testza.AssertEqual(t, 20*time.Second, kp.Timeout)
	testza.AssertFalse(t, kp.PermitWithoutStream)
}
