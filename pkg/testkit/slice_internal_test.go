package testkit

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/MarvinJWendt/testza"
)

// fooRegistry is a stand-in collaborator type for the diagnostic golden tests.
type fooRegistry struct{}

func TestSlice_Validate_UnmetDeclaredDep(t *testing.T) {
	t.Parallel()

	regType := reflect.TypeFor[*fooRegistry]()
	mod := NewMockProviderModule()
	mod.RequiredDeps = []reflect.Type{regType}

	s := NewSlice(t, mod)

	err := s.Validate()
	testza.AssertNotNil(t, err)

	want := fmt.Sprintf(sliceIncompleteFmt, regType, reflect.TypeOf(mod), regType)
	testza.AssertEqual(t, want, err.Error())

	// Validate is side-effect free: no module Init runs.
	testza.AssertEqual(t, int32(0), mod.InitCalls.Load())
}

func TestSlice_Validate_MockModuleCollision(t *testing.T) {
	t.Parallel()

	regType := reflect.TypeFor[*fooRegistry]()
	mod := NewMockProviderModule()
	mod.ProvidesTypes = []reflect.Type{regType}

	s := NewSlice(t, mod)
	Mock[*fooRegistry](s, &fooRegistry{})

	err := s.Validate()
	testza.AssertNotNil(t, err)

	want := fmt.Sprintf(mockCollisionFmt, regType, reflect.TypeOf(mod))
	testza.AssertEqual(t, want, err.Error())

	testza.AssertEqual(t, int32(0), mod.InitCalls.Load())
}
