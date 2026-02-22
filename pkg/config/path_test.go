package config_test

import (
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
)

func TestModulePath_DefaultInstance(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.grpc.server.default", config.ModulePath("grpc", "server", ""))
}

func TestModulePath_NamedInstance(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "modules.grpc.server.internal", config.ModulePath("grpc", "server", "internal"))
}

func TestModulePath_AllCategories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		category, moduleType, instance, want string
	}{
		{config.CategoryGRPC, "server", "default", "modules.grpc.server.default"},
		{config.CategoryHTTP, "fiber", "api", "modules.http.fiber.api"},
		{config.CategoryDB, "pgx", "default", "modules.db.pgx.default"},
		{config.CategoryLogging, "tint", "default", "modules.logging.tint.default"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			testza.AssertEqual(t, tc.want, config.ModulePath(tc.category, tc.moduleType, tc.instance))
		})
	}
}
