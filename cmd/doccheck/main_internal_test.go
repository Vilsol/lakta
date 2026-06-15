package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
)

const (
	fence    = "```"
	stmtMode = "stmt"
	testMode = "test"
)

func TestParseCompileAnnotation_Default(t *testing.T) {
	t.Parallel()

	mode, imports, ok := parseCompileAnnotation("compile")

	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, stmtMode, mode)
	testza.AssertEqual(t, 0, len(imports))
}

func TestParseCompileAnnotation_ModeAndImports(t *testing.T) {
	t.Parallel()

	mode, imports, ok := parseCompileAnnotation(`compile=decl imports="context,github.com/knadh/koanf/v2"`)

	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "decl", mode)
	testza.AssertEqual(t, []string{"context", "github.com/knadh/koanf/v2"}, imports)
}

func TestParseCompileAnnotation_Skip(t *testing.T) {
	t.Parallel()

	mode, _, ok := parseCompileAnnotation("compile=skip")

	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, "skip", mode)
}

func TestParseCompileAnnotation_Absent(t *testing.T) {
	t.Parallel()

	// A plain ```go block with no compile annotation must not be picked up.
	_, _, ok := parseCompileAnnotation("")
	testza.AssertFalse(t, ok)

	_, _, ok = parseCompileAnnotation("json")
	testza.AssertFalse(t, ok)
}

func TestExtractSnippets_OnlyAnnotatedFences(t *testing.T) {
	t.Parallel()

	md := strings.Join([]string{
		"# Title",
		"```go",              // no annotation -> ignored
		"ignored := true",    //
		fence,                //
		"prose",              //
		"```go compile",      // annotated -> captured
		"x := 1",             //
		"y := 2",             //
		fence,                //
		"```go compile=skip", // annotated skip -> captured (mode skip)
		"incomplete",         //
		fence,                //
	}, "\n")

	snippets := extractSnippets("doc.md", bufio.NewScanner(strings.NewReader(md)))

	testza.AssertEqual(t, 2, len(snippets))
	testza.AssertEqual(t, stmtMode, snippets[0].mode)
	testza.AssertEqual(t, "x := 1\ny := 2\n", snippets[0].code)
	testza.AssertEqual(t, "doc.md", snippets[0].srcFile)
	testza.AssertEqual(t, "skip", snippets[1].mode)
}

func TestBuildSource_StmtMode(t *testing.T) {
	t.Parallel()

	src := buildSource(snippet{
		mode:    stmtMode,
		imports: []string{"os"},
		code:    "_ = os.Getenv(\"X\")\n",
	})

	testza.AssertContains(t, src, "package snippet")
	testza.AssertContains(t, src, "\"os\"")
	testza.AssertContains(t, src, "func example() {")
	testza.AssertContains(t, src, "\t_ = os.Getenv(\"X\")")
}

func TestBuildSource_DeclMode(t *testing.T) {
	t.Parallel()

	src := buildSource(snippet{
		mode: "decl",
		code: "type Foo struct{}\n",
	})

	testza.AssertContains(t, src, "package snippet")
	testza.AssertContains(t, src, "type Foo struct{}")
	// decl mode must not wrap code in a function
	testza.AssertFalse(t, strings.Contains(src, "func example()"))
}

func TestBuildSource_TestModeInjectsTesting(t *testing.T) {
	t.Parallel()

	src := buildSource(snippet{
		mode: testMode,
		code: "t.Fatal(\"x\")\n",
	})

	testza.AssertContains(t, src, "\"testing\"")
	testza.AssertContains(t, src, "func testExample(t *testing.T) {")
}

func TestReadModulePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/foo\n\ngo 1.26\n")

	path, err := readModulePath(dir)
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, "example.com/foo", path)
}

func TestReadModulePath_Missing(t *testing.T) {
	t.Parallel()

	_, err := readModulePath(t.TempDir())
	testza.AssertNotNil(t, err)
}

func TestDiscoverModuleDirs_IncludesNestedPkgModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/core\n\ngo 1.26\n")

	nested := filepath.Join(root, "pkg", "http", "fiber")
	testza.AssertNoError(t, os.MkdirAll(nested, dirPerm))
	writeFile(t, filepath.Join(nested, "go.mod"), "module example.com/core/pkg/http/fiber\n\ngo 1.26\n")

	mods, err := discoverModuleDirs(root)
	testza.AssertNoError(t, err)

	paths := make([]string, 0, len(mods))
	for _, m := range mods {
		paths = append(paths, m.path)
	}

	testza.AssertContains(t, paths, "example.com/core")
	testza.AssertContains(t, paths, "example.com/core/pkg/http/fiber")
}

func TestSetupTempModule_WritesRequireAndReplace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/core\n\ngo 1.26\n")
	testza.AssertNoError(t, os.MkdirAll(filepath.Join(root, "pkg"), dirPerm))

	tmp := t.TempDir()
	testza.AssertNoError(t, setupTempModule(tmp, root))

	data, err := os.ReadFile(filepath.Join(tmp, "go.mod")) //nolint:gosec
	testza.AssertNoError(t, err)

	out := string(data)
	testza.AssertContains(t, out, "module doccheck_snippet")
	testza.AssertContains(t, out, "require example.com/core v0.0.0")
	testza.AssertContains(t, out, "replace example.com/core => "+root)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	testza.AssertNoError(t, os.WriteFile(path, []byte(content), filePerm))
}
