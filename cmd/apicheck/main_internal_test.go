package main

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func TestExtractDocumented(t *testing.T) {
	t.Parallel()

	md := "## pkg/lakta\n| `NewRuntime(modules ...Module) *Runtime` | desc |\n| `Provide[T](ctx, fn)` | desc |\nplain prose NotDocumented here"

	got := extractDocumented(md)

	testza.AssertTrue(t, got["NewRuntime"])
	testza.AssertTrue(t, got["Runtime"])
	testza.AssertTrue(t, got["Module"])
	testza.AssertTrue(t, got["Provide"])
	// prose outside code spans must not count
	testza.AssertFalse(t, got["NotDocumented"])
}

func TestExtractIgnored(t *testing.T) {
	t.Parallel()

	md := "intro\n<!-- apicheck:ignore\npkg/config: Config, Option, NewDefaultConfig\npkg/lakta: Dependent\n-->\nrest"

	got := extractIgnored(md)

	testza.AssertTrue(t, got["Config"])
	testza.AssertTrue(t, got["Option"])
	testza.AssertTrue(t, got["NewDefaultConfig"])
	testza.AssertTrue(t, got["Dependent"])
	testza.AssertFalse(t, got["NotListed"])
}

func TestExtractIgnored_Absent(t *testing.T) {
	t.Parallel()

	got := extractIgnored("no comment here")
	testza.AssertEqual(t, 0, len(got))
}

func TestExportedFromDecl(t *testing.T) {
	t.Parallel()

	src := `package p
func Exported() {}
func unexported() {}
func (m *Module) Method() {}
type PublicType struct{}
type privateType struct{}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	testza.AssertNoError(t, err)

	got := make([]string, 0, len(f.Decls))
	for _, d := range f.Decls {
		got = append(got, exportedFromDecl(d)...)
	}

	testza.AssertContains(t, got, "Exported")
	testza.AssertContains(t, got, "PublicType")
	testza.AssertNotContains(t, got, "unexported")
	testza.AssertNotContains(t, got, "privateType")
	// methods are not top-level symbols
	testza.AssertNotContains(t, got, "Method")
}
