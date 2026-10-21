package authconnect

import (
	"context"

	"connectrpc.com/connect"
	connectmod "github.com/Vilsol/lakta/pkg/http/connect"
)

// Authorizer fills the authz slot and learns the mounted services. An
// authorizer that also has IsPublic(procedure string) bool marks procedures
// that accept calls without a token.
type Authorizer interface {
	Slot() func(ctx context.Context) (connect.Interceptor, error)
	Mounted(ctx context.Context, services []connectmod.MountedService) error
}

type publicProcedures interface {
	IsPublic(procedure string) bool
}

// WithAuthorizer sets SlotAuth to New(opts...), with WithPublicProcedures(a.IsPublic)
// first when a has IsPublic so opts can override it, sets SlotAuthz to a.Slot()
// and adds a.Mounted as a mounted hook. A nil a adds nothing; combine with
// connectmod.WithRequiredSlots so a missing authorizer fails Init.
func WithAuthorizer(a Authorizer, opts ...Option) connectmod.Option {
	return func(c *connectmod.Config) {
		if a == nil {
			return
		}
		if p, ok := a.(publicProcedures); ok {
			opts = append([]Option{WithPublicProcedures(p.IsPublic)}, opts...)
		}
		connectmod.WithSlot(connectmod.SlotAuth, New(opts...))(c)
		connectmod.WithSlot(connectmod.SlotAuthz, a.Slot())(c)
		connectmod.WithMountedHook(a.Mounted)(c)
	}
}
