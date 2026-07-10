package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func parseFiles(t *testing.T, sources ...string) []*ast.File {
	t.Helper()

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))

	for i, src := range sources {
		f, err := parser.ParseFile(fset, "f", src, 0)
		testza.AssertNoError(t, err, "source %d should parse", i)
		files = append(files, f)
	}

	return files
}

func TestQualifies_ModuleShape(t *testing.T) {
	t.Parallel()

	files := parseFiles(t,
		`package m
func NewDefaultConfig() Config { return Config{} }`,
		`package m
func (m *Module) ConfigPath() string { return "modules.x.y" }`,
	)

	testza.AssertTrue(t, qualifies(files))
}

func TestQualifies_ConfigLoaderExcluded(t *testing.T) {
	t.Parallel()

	// pkg/config shape: NewDefaultConfig exists, but ConfigPath is on a generic
	// bindModule, not the concrete Module type.
	files := parseFiles(t,
		`package config
func NewDefaultConfig() Config { return Config{} }`,
		`package config
func (m *bindModule[T]) ConfigPath() string { return "" }`,
	)

	testza.AssertFalse(t, qualifies(files))
}

func TestQualifies_MissingConfigPath(t *testing.T) {
	t.Parallel()

	files := parseFiles(t, `package m
func NewDefaultConfig() Config { return Config{} }`)

	testza.AssertFalse(t, qualifies(files))
}

func TestQualifies_MissingNewDefaultConfig(t *testing.T) {
	t.Parallel()

	files := parseFiles(t, `package m
func (m *Module) ConfigPath() string { return "" }`)

	testza.AssertFalse(t, qualifies(files))
}

func TestAliasFor(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "pkg_grpc_server", aliasFor("pkg/grpc/server"))
	testza.AssertEqual(t, "pkg_db_drivers_pgx", aliasFor("pkg/db/drivers/pgx"))
	testza.AssertEqual(t, "pkg_logging_slog", aliasFor("pkg/logging/slog"))
}

func TestRender_FormatsAndIncludesCalls(t *testing.T) {
	t.Parallel()

	src, err := render([]module{
		{importPath: "github.com/Vilsol/lakta/pkg/grpc/server", alias: "pkg_grpc_server"},
	})
	testza.AssertNoError(t, err)

	out := string(src)
	testza.AssertContains(t, out, "DO NOT EDIT")
	testza.AssertContains(t, out, `pkg_grpc_server "github.com/Vilsol/lakta/pkg/grpc/server"`)
	testza.AssertContains(t, out, "reflectcfg.FromModule(pkg_grpc_server.NewModule(), pkg_grpc_server.NewDefaultConfig()),")
}
