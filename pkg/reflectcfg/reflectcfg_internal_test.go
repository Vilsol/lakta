package reflectcfg

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func TestInferCategoryAndType(t *testing.T) {
	t.Parallel()

	cat, typ := inferCategoryAndType("github.com/Vilsol/lakta/pkg/grpc/server")
	testza.AssertEqual(t, "grpc", cat)
	testza.AssertEqual(t, "server", typ)

	cat, typ = inferCategoryAndType("github.com/Vilsol/lakta/pkg/db/drivers/pgx")
	testza.AssertEqual(t, "db", cat)
	testza.AssertEqual(t, "pgx", typ)

	// single segment under pkg: category == type
	cat, typ = inferCategoryAndType("github.com/Vilsol/lakta/pkg/otel")
	testza.AssertEqual(t, "otel", cat)
	testza.AssertEqual(t, "otel", typ)

	// no /pkg/ segment: returns path for both
	cat, typ = inferCategoryAndType("example.com/standalone")
	testza.AssertEqual(t, "example.com/standalone", cat)
	testza.AssertEqual(t, "example.com/standalone", typ)
}

func TestCategoryAndType(t *testing.T) {
	t.Parallel()

	// canonical module path wins over package-path inference
	cat, typ := categoryAndType("modules.custom.widget.default", "example.com/svc/internal/widget")
	testza.AssertEqual(t, "custom", cat)
	testza.AssertEqual(t, "widget", typ)

	// empty path falls back to inference
	cat, typ = categoryAndType("", "github.com/Vilsol/lakta/pkg/grpc/server")
	testza.AssertEqual(t, "grpc", cat)
	testza.AssertEqual(t, "server", typ)

	// non-canonical path (wrong segment count / prefix) falls back too
	cat, typ = categoryAndType("widget.default", "github.com/Vilsol/lakta/pkg/otel")
	testza.AssertEqual(t, "otel", cat)
	testza.AssertEqual(t, "otel", typ)
}

func TestPackageDirs(t *testing.T) {
	t.Parallel()

	// own package resolves to the current directory; unknown packages are
	// skipped (-e) instead of failing the batch.
	dirs, err := packageDirs([]string{"github.com/Vilsol/lakta/pkg/reflectcfg", "example.com/does/not/exist"})
	testza.AssertNoError(t, err)

	wd, err := os.Getwd()
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, wd, dirs["github.com/Vilsol/lakta/pkg/reflectcfg"])

	_, ok := dirs["example.com/does/not/exist"]
	testza.AssertFalse(t, ok)
}

func TestEnvVarName(t *testing.T) {
	t.Parallel()

	got := envVarName("modules.grpc.server.<name>", "port")
	testza.AssertEqual(t, "LAKTA_MODULES__GRPC__SERVER__<NAME>__PORT", got)
}

func TestPkgAlias(t *testing.T) {
	t.Parallel()

	// version suffix is skipped in favor of the real package name
	testza.AssertEqual(t, "fiber", pkgAlias("github.com/gofiber/fiber/v3"))
	testza.AssertEqual(t, "koanf", pkgAlias("github.com/knadh/koanf/v2"))
	// hyphenated names keep only the part before the hyphen
	testza.AssertEqual(t, "health", pkgAlias("github.com/hellofresh/health-go/v5"))
	// plain name passes through
	testza.AssertEqual(t, "slog", pkgAlias("log/slog"))
}

func TestIsBuiltin(t *testing.T) {
	t.Parallel()

	testza.AssertTrue(t, isBuiltin("string"))
	testza.AssertTrue(t, isBuiltin("int"))
	testza.AssertTrue(t, isBuiltin("error"))
	testza.AssertFalse(t, isBuiltin("Config"))
	testza.AssertFalse(t, isBuiltin("Duration"))
}

func TestFormatType(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, "string", formatType(reflect.TypeFor[string]()))
	testza.AssertEqual(t, "int", formatType(reflect.TypeFor[int]()))
	testza.AssertEqual(t, "[]string", formatType(reflect.TypeFor[[]string]()))
	testza.AssertEqual(t, "*int", formatType(reflect.TypeFor[*int]()))
	testza.AssertEqual(t, "map[string]int", formatType(reflect.TypeFor[map[string]int]()))
	// empty interface renders as any
	testza.AssertEqual(t, "any", formatType(reflect.TypeFor[any]()))
}

func TestDefaultValue(t *testing.T) {
	t.Parallel()

	// zero values render as empty string
	testza.AssertEqual(t, "", defaultValue(reflect.ValueOf("")))
	testza.AssertEqual(t, "", defaultValue(reflect.ValueOf(0)))
	// non-zero values render via %v
	testza.AssertEqual(t, "50051", defaultValue(reflect.ValueOf(50051)))
	testza.AssertEqual(t, "0.0.0.0", defaultValue(reflect.ValueOf("0.0.0.0")))
	testza.AssertEqual(t, "true", defaultValue(reflect.ValueOf(true)))
}

func TestCleanComment(t *testing.T) {
	t.Parallel()

	// strips "FuncName " prefix, lowercases, drops trailing period
	testza.AssertEqual(t, "sets the port number", cleanComment("WithPort sets the port number."))
	// strips "Config " prefix
	testza.AssertEqual(t, "holds the settings", cleanComment("Config holds the settings."))
	// only the first line is kept
	testza.AssertEqual(t, "first line", cleanComment("first line\nsecond line"))
	// prefix that is not With/Config is preserved
	testza.AssertEqual(t, "host to bind", cleanComment("host to bind"))
	// acronym-leading comments keep their casing (no "jWKSURL" mangling)
	testza.AssertEqual(t, "JWKSURL is the JWKS endpoint", cleanComment("JWKSURL is the JWKS endpoint."))
	testza.AssertEqual(t, "TTL bounds entry lifetime", cleanComment("TTL bounds entry lifetime."))
}

func TestExtractStructComments(t *testing.T) {
	t.Parallel()

	src := `package p
// Config holds the module settings.
type Config struct {
	// Host is the bind address.
	Host string ` + "`koanf:\"host\"`" + `
	Port int ` + "`koanf:\"port\"`" + ` // Port is the listen port.
	unexported string
}`

	sc := parseStructComments(t, src)

	testza.AssertEqual(t, "holds the module settings", sc.structDoc)
	// only With-prefixed funcs and "Config" get their prefix stripped; field
	// names are merely first-letter-lowercased.
	testza.AssertEqual(t, "host is the bind address", sc.fields["Host"])
	// inline comment fallback
	testza.AssertEqual(t, "port is the listen port", sc.fields["Port"])
	// unexported fields are not recorded
	_, ok := sc.fields["unexported"]
	testza.AssertFalse(t, ok)
}

func TestExtractFuncComment(t *testing.T) {
	t.Parallel()

	src := `package p
// WithPort sets the listen port.
func WithPort(p int) Option { return nil }
// NewModule constructs the module.
func NewModule() *Module { return nil }`

	sc := sourceComments{fields: map[string]string{}, funcs: map[string]string{}}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	testza.AssertNoError(t, err)

	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			extractFuncComment(fd, &sc)
		}
	}

	testza.AssertEqual(t, "sets the listen port", sc.funcs["WithPort"])
	// only WithXxx funcs are recorded
	_, ok := sc.funcs["NewModule"]
	testza.AssertFalse(t, ok)
}

func parseStructComments(t *testing.T, src string) sourceComments {
	t.Helper()

	sc := sourceComments{fields: map[string]string{}, funcs: map[string]string{}}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	testza.AssertNoError(t, err)

	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok {
			extractStructComments(gd, &sc)
		}
	}

	return sc
}
