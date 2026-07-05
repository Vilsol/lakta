package lakta_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
)

func TestRenderWiringReport(t *testing.T) {
	t.Parallel()

	info := []lakta.ModuleInfo{
		{
			Type:         "config.Module",
			InitOrder:    0,
			Provides:     []string{"*koanf.Koanf"},
			Lifecycle:    lakta.LifecycleInit,
			State:        lakta.StateInitialized,
			InitDuration: 2 * time.Millisecond,
		},
		{
			Type:         "fiberserver.Module",
			Name:         "public",
			InitOrder:    1,
			Requires:     []string{"*koanf.Koanf"},
			Lifecycle:    lakta.LifecycleSync,
			State:        lakta.StateStarted,
			InitDuration: 5 * time.Millisecond,
		},
	}

	out := lakta.RenderWiringReport(info, nil)
	testza.AssertTrue(t, strings.Contains(out, "ORDER"))
	testza.AssertTrue(t, strings.Contains(out, "config.Module"))
	testza.AssertTrue(t, strings.Contains(out, "fiberserver.Module (public)"))
	testza.AssertTrue(t, strings.Contains(out, "sync"))
	testza.AssertTrue(t, strings.Contains(out, "*koanf.Koanf"))
	testza.AssertFalse(t, strings.Contains(out, "config provenance"))

	withProv := lakta.RenderWiringReport(info, map[string]string{"a.b": "file"})
	testza.AssertTrue(t, strings.Contains(withProv, "config provenance"))
	testza.AssertTrue(t, strings.Contains(withProv, "a.b = file"))
}
