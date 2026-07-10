package grpc_test

import (
	"context"
	stderrors "errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	authgrpc "github.com/Vilsol/lakta/pkg/auth/grpc"
	"github.com/Vilsol/lakta/pkg/auth/verifier"
	errpkg "github.com/Vilsol/lakta/pkg/errors"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/selector"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

const testSecret = "grpc-adapter-dev-secret" //nolint:gosec // test-only fixture secret

const methodGet = "/pkg.Service/Get"

func TestMain(m *testing.M) {
	_ = os.Setenv("LAKTA_PROFILE", "test")
	os.Exit(m.Run())
}

func testRegistry(t *testing.T) *verifier.Registry {
	t.Helper()
	h := testkit.NewHarness(t)
	k := koanf.New(".")
	testza.AssertNoError(t, k.Load(confmap.Provider(map[string]any{
		"modules.auth.verifier.default.static_key.secret":   testSecret,
		"modules.auth.verifier.default.static_key.profiles": []string{"test"},
		"modules.auth.verifier.default.scope_claim":         "scope",
		"modules.auth.verifier.default.roles_claim":         "roles",
	}, "."), nil))
	mod := verifier.NewModule()
	testza.AssertNoError(t, mod.LoadConfig(k))
	testza.AssertNoError(t, mod.Init(h.Ctx()))
	t.Cleanup(func() { _ = mod.Shutdown(h.Ctx()) })
	reg, err := lakta.Invoke[*verifier.Registry](h.Ctx())
	testza.AssertNoError(t, err)
	return reg
}

func bearerCtx(t *testing.T, scopes, roles []string) context.Context {
	t.Helper()
	now := time.Now()
	b := jwt.NewBuilder().Issuer("dev-cli").Subject("user-1").IssuedAt(now).Expiration(now.Add(time.Hour))
	if scopes != nil {
		b = b.Claim("scope", spaceJoin(scopes))
	}
	if roles != nil {
		b = b.Claim("roles", anySlice(roles))
	}
	tok, err := b.Build()
	testza.AssertNoError(t, err)
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.HS256(), []byte(testSecret)))
	testza.AssertNoError(t, err)
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+string(signed)))
}

func spaceJoin(in []string) string {
	out := ""
	var outSb72 strings.Builder
	for i, s := range in {
		if i > 0 {
			outSb72.WriteString(" ")
		}
		outSb72.WriteString(s)
	}
	out += outSb72.String()
	return out
}

func anySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func appCode(t *testing.T, err error) codes.Code {
	t.Helper()
	var appErr *errpkg.AppError
	testza.AssertTrue(t, stderrors.As(err, &appErr))
	return appErr.GRPC
}

func TestUnaryInterceptor_ValidToken_SetsPrincipal(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	ic, err := authgrpc.NewUnaryServerInterceptor(reg, "default")
	testza.AssertNoError(t, err)

	var gotSubject string
	handler := func(ctx context.Context, _ any) (any, error) {
		p, ok := verifier.PrincipalFrom(ctx)
		testza.AssertTrue(t, ok)
		gotSubject = p.Subject
		return "ok", nil
	}
	_, err = ic(bearerCtx(t, []string{"read"}, nil), nil, &grpc.UnaryServerInfo{FullMethod: methodGet}, handler)
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "user-1", gotSubject)
}

func TestUnaryInterceptor_NoToken_Unauthenticated(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	ic, err := authgrpc.NewUnaryServerInterceptor(reg, "default")
	testza.AssertNoError(t, err)

	called := false
	handler := func(context.Context, any) (any, error) { called = true; return nil, nil }
	_, err = ic(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: methodGet}, handler)
	testza.AssertEqual(t, codes.Unauthenticated, appCode(t, err))
	testza.AssertFalse(t, called)
}

func TestRequireScope_PermissionDenied(t *testing.T) {
	t.Parallel()
	ctx := verifier.ContextWithPrincipal(context.Background(), &verifier.Principal{Scopes: []string{"read"}})
	err := authgrpc.RequireScope(ctx, "admin")
	testza.AssertEqual(t, codes.PermissionDenied, appCode(t, err))

	testza.AssertNoError(t, authgrpc.RequireScope(ctx, "read"))
}

func TestRequireRole_PermissionDenied(t *testing.T) {
	t.Parallel()
	ctx := verifier.ContextWithPrincipal(context.Background(), &verifier.Principal{Roles: []string{"viewer"}})
	err := authgrpc.RequireRole(ctx, "editor")
	testza.AssertEqual(t, codes.PermissionDenied, appCode(t, err))
	testza.AssertNoError(t, authgrpc.RequireRole(ctx, "viewer"))
}

func TestRequireScope_NoPrincipal_Unauthenticated(t *testing.T) {
	t.Parallel()
	err := authgrpc.RequireScope(context.Background(), "admin")
	testza.AssertEqual(t, codes.Unauthenticated, appCode(t, err))
}

func TestSelector_ExcludedMethodPassesAnonymously(t *testing.T) {
	t.Parallel()
	reg := testRegistry(t)
	ic, err := authgrpc.NewUnaryServerInterceptor(reg, "default")
	testza.AssertNoError(t, err)

	// Skip auth for the Login method; enforce it everywhere else.
	guarded := selector.UnaryServerInterceptor(ic, selector.MatchFunc(func(_ context.Context, c interceptors.CallMeta) bool {
		return c.FullMethod() != "/pkg.Service/Login"
	}))

	loginCalled := false
	handler := func(context.Context, any) (any, error) { loginCalled = true; return "ok", nil }

	// Anonymous call to the excluded method runs the handler.
	_, err = guarded(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/pkg.Service/Login"}, handler)
	testza.AssertNoError(t, err)
	testza.AssertTrue(t, loginCalled)

	// Anonymous call to a protected method is rejected.
	_, err = guarded(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: methodGet}, handler)
	testza.AssertEqual(t, codes.Unauthenticated, appCode(t, err))
}
