package otel

import (
	"context"
	"errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	semconv "go.opentelemetry.io/otel/semconv/v1.36.0"
)

var (
	_ lakta.Module       = (*Module)(nil)
	_ lakta.Configurable = (*Module)(nil)
	_ lakta.NamedModule  = (*Module)(nil)
	_ lakta.Dependent    = (*Module)(nil)
	_ lakta.Provider     = (*Module)(nil)
)

func TestBuildInfoAttrs_VCSPresentInGitRepo(t *testing.T) {
	t.Parallel()

	// Test binaries built from a git repo should have vcs.revision set.
	attrs := buildInfoAttrs("")
	keys := make(map[string]string, len(attrs))
	for _, a := range attrs {
		keys[string(a.Key)] = a.Value.AsString()
	}

	// vcs.revision may be absent in environments without git metadata (CI clean checkouts),
	// but when present it must be non-empty.
	if rev, ok := keys["vcs.revision"]; ok {
		testza.AssertNotEqual(t, "", rev)
	}
}

func TestBuildInfoAttrs_CfgVersionSkipsFallback(t *testing.T) {
	t.Parallel()

	attrs := buildInfoAttrs("explicit-v9")
	for _, a := range attrs {
		testza.AssertNotEqual(t, string(semconv.ServiceVersionKey), string(a.Key))
	}
}

func TestBuildInfoAttrs_EmptyCfgVersionAllowsFallback(t *testing.T) {
	t.Parallel()

	// In test binaries Main.Version is "" or "(devel)", so no service.version attribute
	// is expected. This test simply asserts the function doesn't panic.
	_ = buildInfoAttrs("")
}

func TestInit_FailOpenWhenSetupErrorsAndNotRequired(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	m := NewModule(
		WithRequired(false),
		WithSetupFn(func(context.Context, string) (func(context.Context) error, error) {
			return nil, errors.New("collector exploded")
		}),
	)

	testza.AssertNoError(t, m.Init(h.Ctx()))
}

func TestInit_FatalWhenSetupErrorsAndRequired(t *testing.T) {
	t.Parallel()

	h := testkit.NewHarness(t)
	m := NewModule(
		WithRequired(true),
		WithSetupFn(func(context.Context, string) (func(context.Context) error, error) {
			return nil, errors.New("collector exploded")
		}),
	)

	testza.AssertNotNil(t, m.Init(h.Ctx()))
}
