package authconnect_test

import (
	"context"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/MarvinJWendt/testza"
	authconnect "github.com/Vilsol/lakta/pkg/auth/connect"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
	"github.com/Vilsol/lakta/pkg/lakta"
)

// fakeAuthorizer denies privateRPC in the authz slot and counts Mounted calls.
type fakeAuthorizer struct {
	mounted atomic.Int32
}

func (f *fakeAuthorizer) Slot() func(ctx context.Context) (connect.Interceptor, error) {
	return func(context.Context) (connect.Interceptor, error) {
		return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				if req.Spec().Procedure == privateRPC {
					return nil, connect.NewError(connect.CodePermissionDenied, nil)
				}
				return next(ctx, req)
			}
		}), nil
	}
}

func (f *fakeAuthorizer) Mounted(context.Context, []connectmod.MountedService) error {
	f.mounted.Add(1)
	return nil
}

type publicFake struct {
	fakeAuthorizer
}

func (*publicFake) IsPublic(p string) bool { return p == publicRPC }

func TestWithAuthorizer_FillsAuthAuthzAndMountedHook(t *testing.T) {
	t.Parallel()

	a := &publicFake{}
	e := startWith(t, true, authconnect.WithAuthorizer(a))

	got, err := call(t, e.base, publicRPC, "")
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "anonymous", got)

	_, err = call(t, e.base, privateRPC, "")
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	_, err = call(t, e.base, privateRPC, e.token())
	testza.AssertEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))

	testza.AssertEqual(t, int32(1), a.mounted.Load())
}

func TestWithAuthorizer_WithoutIsPublicEveryProcedureNeedsAToken(t *testing.T) {
	t.Parallel()

	e := startWith(t, true, authconnect.WithAuthorizer(&fakeAuthorizer{}))

	_, err := call(t, e.base, publicRPC, "")
	testza.AssertEqual(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestWithAuthorizer_NilWithRequiredSlotsFailsInit(t *testing.T) {
	t.Parallel()

	m := connectmod.NewModule(
		connectmod.WithHost("127.0.0.1"),
		connectmod.WithPort(0),
		authconnect.WithAuthorizer(nil),
		connectmod.WithRequiredSlots(connectmod.SlotAuth, connectmod.SlotAuthz),
	)
	err := lakta.NewRuntime(m).RunContext(t.Context())
	testza.AssertNotNil(t, err)
	testza.AssertContains(t, err.Error(), "required")
}
