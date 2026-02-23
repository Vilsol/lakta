package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Annotation format in fenced code block info string:
//
//	```go compile
//	```go compile=stmt imports="os,github.com/Vilsol/lakta/pkg/config"
//	```go compile=decl imports="context,github.com/knadh/koanf/v2"
//	```go compile=skip
//	```go compile=decl stubs="TypeA,TypeB"
//	```go compile=decl group="my-group"
//
// Modes:
//   - stmt (default): wrap code in func example() { ... }
//   - decl: just package + imports (for type/func declarations)
//   - skip: extract but do not compile (documented as intentionally incomplete)
//
// Extras:
//   - stubs="TypeA,TypeB": generate empty struct stubs (type X struct{} + func NewX(...any)*X)
//   - group="name": combine all blocks with the same group into one compilation unit

const (
	minArgs  = 2
	dirPerm  = 0o750
	filePerm = 0o600
)

func main() {
	if len(os.Args) < minArgs {
		fmt.Fprintln(os.Stderr, "usage: doccheck <markdown-file...>")
		os.Exit(1)
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to get working directory:", err)
		os.Exit(1)
	}

	tmpDir, err := os.MkdirTemp("", "doccheck")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create temp dir:", err)
		os.Exit(1)
	}

	if err := setupTempModule(tmpDir, repoRoot); err != nil {
		fmt.Fprintln(os.Stderr, "failed to set up temp module:", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	failed := false

	for _, path := range os.Args[1:] {
		if checkErr := checkFile(path, tmpDir); checkErr != nil {
			fmt.Fprintln(os.Stderr, checkErr)
			failed = true
		}
	}

	_ = os.RemoveAll(tmpDir)

	if failed {
		os.Exit(1)
	}
}

func setupTempModule(tmpDir, repoRoot string) error {
	goMod := fmt.Sprintf(`module doccheck_snippet

go 1.26

require github.com/Vilsol/lakta v0.0.0

replace github.com/Vilsol/lakta => %s
`, repoRoot)

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), filePerm); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	return copyFile(filepath.Join(repoRoot, "go.sum"), filepath.Join(tmpDir, "go.sum"))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, filePerm) //nolint:gosec
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}

	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}

	return nil
}

type snippet struct {
	srcFile string
	srcLine int
	mode    string
	imports []string
	stubs   []string
	group   string
	code    string
}

func checkFile(path, tmpDir string) error {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	snippets := resolveGroups(extractSnippets(path, bufio.NewScanner(f)))

	failures := 0

	for i, s := range snippets {
		if s.mode == "skip" {
			continue
		}

		if compileErr := compileSnippet(i, s, tmpDir); compileErr != nil {
			fmt.Fprintf(os.Stderr, "%s:%d: compile error:\n%s\n", s.srcFile, s.srcLine, compileErr)
			failures++
		}
	}

	if failures > 0 {
		return fmt.Errorf("%s: %d snippet(s) failed", path, failures)
	}

	return nil
}

// resolveGroups combines snippets sharing the same group name into a single decl-mode
// snippet placed where the group's first member appeared, merging imports, stubs, and code.
func resolveGroups(snippets []snippet) []snippet {
	type groupState struct {
		insertIdx int
		members   []snippet
	}

	groups := make(map[string]*groupState)
	out := make([]snippet, 0, len(snippets))

	for _, s := range snippets {
		if s.group == "" {
			out = append(out, s)
			continue
		}

		if gs, ok := groups[s.group]; ok {
			gs.members = append(gs.members, s)
		} else {
			groups[s.group] = &groupState{insertIdx: len(out), members: []snippet{s}}
			out = append(out, snippet{}) // placeholder
		}
	}

	for _, gs := range groups {
		out[gs.insertIdx] = combineSnippets(gs.members)
	}

	return out
}

func combineSnippets(group []snippet) snippet {
	combined := snippet{
		srcFile: group[0].srcFile,
		srcLine: group[0].srcLine,
		mode:    "decl",
	}

	seen := make(map[string]bool)

	for _, s := range group {
		for _, imp := range s.imports {
			if !seen["i:"+imp] {
				seen["i:"+imp] = true
				combined.imports = append(combined.imports, imp)
			}
		}

		for _, stub := range s.stubs {
			if !seen["s:"+stub] {
				seen["s:"+stub] = true
				combined.stubs = append(combined.stubs, stub)
			}
		}

		combined.code += stripPackageAndImports(s.code)
	}

	return combined
}

// stripPackageAndImports removes package declarations and import blocks from code
// so it can be safely merged into a combined compilation unit.
func stripPackageAndImports(code string) string {
	var out strings.Builder

	inImportBlock := false

	for line := range strings.SplitSeq(code, "\n") {
		trimmed := strings.TrimSpace(line)

		if inImportBlock {
			if trimmed == ")" {
				inImportBlock = false
			}

			continue
		}

		if strings.HasPrefix(trimmed, "package ") {
			continue
		}

		if trimmed == "import (" {
			inImportBlock = true
			continue
		}

		if strings.HasPrefix(trimmed, `import "`) {
			continue
		}

		out.WriteString(line)
		out.WriteString("\n")
	}

	return out.String()
}

func extractSnippets(path string, scanner *bufio.Scanner) []snippet {
	var snippets []snippet

	lineNum := 0
	inFence := false

	var current *snippet

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if !inFence {
			if info, found := strings.CutPrefix(line, "```go"); found {
				mode, imports, stubs, group, ok := parseCompileAnnotation(strings.TrimSpace(info))

				if ok {
					current = &snippet{
						srcFile: path,
						srcLine: lineNum,
						mode:    mode,
						imports: imports,
						stubs:   stubs,
						group:   group,
					}

					inFence = true
				}
			}

			continue
		}

		if strings.TrimSpace(line) == "```" {
			inFence = false

			if current != nil {
				snippets = append(snippets, *current)
				current = nil
			}

			continue
		}

		if current != nil {
			current.code += line + "\n"
		}
	}

	return snippets
}

// parseCompileAnnotation parses the info string after ```go.
// Returns mode, imports, stubs, group, and whether the compile annotation was present.
func parseCompileAnnotation(info string) (string, []string, []string, string, bool) {
	parts := strings.Fields(info)
	compileIdx := -1

	for i, p := range parts {
		if p == "compile" || strings.HasPrefix(p, "compile=") {
			compileIdx = i
			break
		}
	}

	if compileIdx < 0 {
		return "", nil, nil, "", false
	}

	compile := parts[compileIdx]

	var mode string

	if compile == "compile" {
		mode = "stmt"
	} else {
		mode = strings.TrimPrefix(compile, "compile=")
	}

	var imports, stubs []string
	var group string

	for _, p := range parts[compileIdx+1:] {
		if raw, found := strings.CutPrefix(p, "imports="); found {
			raw = strings.Trim(raw, `"`)

			for imp := range strings.SplitSeq(raw, ",") {
				if imp = strings.TrimSpace(imp); imp != "" {
					imports = append(imports, imp)
				}
			}
		}

		if raw, found := strings.CutPrefix(p, "stubs="); found {
			raw = strings.Trim(raw, `"`)

			for stub := range strings.SplitSeq(raw, ",") {
				if stub = strings.TrimSpace(stub); stub != "" {
					stubs = append(stubs, stub)
				}
			}
		}

		if raw, found := strings.CutPrefix(p, "group="); found {
			group = strings.Trim(raw, `"`)
		}
	}

	return mode, imports, stubs, group, true
}

func compileSnippet(index int, s snippet, tmpDir string) error {
	src := buildSource(s)
	pkgName := fmt.Sprintf("snippet_%d", index)
	dir := filepath.Join(tmpDir, pkgName)

	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	srcPath := filepath.Join(dir, "main.go")

	if err := os.WriteFile(srcPath, []byte(src), filePerm); err != nil {
		return fmt.Errorf("write snippet: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), "go", "build", "-mod=mod", "./"+pkgName) //nolint:gosec
	cmd.Dir = tmpDir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		msg := strings.ReplaceAll(out.String(), srcPath+":", "generated:")
		return fmt.Errorf("%s", strings.TrimSpace(msg))
	}

	return nil
}

func buildSource(s snippet) string {
	var b strings.Builder

	b.WriteString("package snippet\n")

	imports := s.imports
	if s.mode == "test" {
		imports = append([]string{"testing"}, imports...)
	}

	if len(imports) > 0 {
		b.WriteString("\nimport (\n")

		for _, imp := range imports {
			b.WriteString("\t\"")
			b.WriteString(imp)
			b.WriteString("\"\n")
		}

		b.WriteString(")\n")
	}

	b.WriteString("\n")

	// Stub declarations are package-level, placed before mode-specific code.
	seen := make(map[string]bool)

	for _, stub := range s.stubs {
		if seen[stub] {
			continue
		}

		seen[stub] = true

		b.WriteString("type ")
		b.WriteString(stub)
		b.WriteString(" struct{}\n")
		b.WriteString("func New")
		b.WriteString(stub)
		b.WriteString("(args ...any) *")
		b.WriteString(stub)
		b.WriteString(" { return nil }\n")
	}

	if len(s.stubs) > 0 {
		b.WriteString("\n")
	}

	switch s.mode {
	case "stmt":
		b.WriteString("func example() {\n")

		for line := range strings.SplitSeq(strings.TrimRight(s.code, "\n"), "\n") {
			b.WriteString("\t")
			b.WriteString(line)
			b.WriteString("\n")
		}

		b.WriteString("}\n")
	case "test":
		b.WriteString("func testExample(t *testing.T) {\n")

		for line := range strings.SplitSeq(strings.TrimRight(s.code, "\n"), "\n") {
			b.WriteString("\t")
			b.WriteString(line)
			b.WriteString("\n")
		}

		b.WriteString("}\n")
	case "decl":
		b.WriteString(s.code)
	}

	return b.String()
}
