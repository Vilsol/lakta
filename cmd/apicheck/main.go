// Command apicheck guards the hand-curated API index against drift: it fails
// when an exported top-level func or type in a tracked package is neither listed
// in reference/api-index.md nor explicitly ignored. Intentional omissions live
// in an `apicheck:ignore` HTML comment inside the index, so adding a new public
// symbol forces a conscious choice — document it or ignore it.
package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const indexPath = "docs/src/content/docs/reference/api-index.md"

func main() {
	trackedPackages := []string{
		"pkg/lakta",
		"pkg/config",
		"pkg/testkit",
	}

	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "apicheck:", err)
		os.Exit(1)
	}

	md, err := os.ReadFile(filepath.Join(root, indexPath)) //nolint:gosec
	if err != nil {
		fmt.Fprintln(os.Stderr, "apicheck:", err)
		os.Exit(1)
	}

	documented := extractDocumented(string(md))
	ignored := extractIgnored(string(md))

	var missing []string

	for _, pkg := range trackedPackages {
		syms, symErr := exportedSymbols(filepath.Join(root, pkg))
		if symErr != nil {
			fmt.Fprintln(os.Stderr, "apicheck:", symErr)
			os.Exit(1)
		}

		for _, s := range syms {
			if documented[s] || ignored[s] {
				continue
			}

			missing = append(missing, pkg+"."+s)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		fmt.Fprintln(os.Stderr, "apicheck: exported symbols missing from "+indexPath+" (document them, or add to the apicheck:ignore comment):")

		for _, m := range missing {
			fmt.Fprintln(os.Stderr, "  "+m)
		}

		os.Exit(1)
	}

	_, _ = fmt.Fprintln(os.Stdout, "apicheck: API index is up to date")
}

var (
	codeSpanRe = regexp.MustCompile("`([^`]+)`")
	identRe    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	ignoreRe   = regexp.MustCompile(`(?s)<!--\s*apicheck:ignore(.*?)-->`)
)

// extractDocumented returns the set of identifiers that appear inside any
// backtick code span in the markdown. A symbol counts as documented when its
// exact name shows up in a code span (e.g. `NewRuntime(...) *Runtime` documents
// both NewRuntime and Runtime).
func extractDocumented(md string) map[string]bool {
	set := make(map[string]bool)

	for _, m := range codeSpanRe.FindAllStringSubmatch(md, -1) {
		for _, id := range identRe.FindAllString(m[1], -1) {
			set[id] = true
		}
	}

	return set
}

// extractIgnored returns identifiers listed in the apicheck:ignore HTML comment.
func extractIgnored(md string) map[string]bool {
	set := make(map[string]bool)

	m := ignoreRe.FindStringSubmatch(md)
	if m == nil {
		return set
	}

	for _, id := range identRe.FindAllString(m[1], -1) {
		set[id] = true
	}

	return set
}

// exportedSymbols returns exported top-level func and type names declared in the
// non-test Go files of a package directory.
func exportedSymbols(dir string) ([]string, error) {
	fset := token.NewFileSet()

	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool { //nolint:staticcheck
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", dir, err)
	}

	var syms []string

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				syms = append(syms, exportedFromDecl(decl)...)
			}
		}
	}

	sort.Strings(syms)

	return syms, nil
}

func exportedFromDecl(decl ast.Decl) []string {
	var syms []string

	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil && d.Name.IsExported() {
			syms = append(syms, d.Name.Name)
		}
	case *ast.GenDecl:
		if d.Tok != token.TYPE {
			return nil
		}

		for _, spec := range d.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.IsExported() {
				syms = append(syms, ts.Name.Name)
			}
		}
	}

	return syms
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found from working directory")
		}

		dir = parent
	}
}
