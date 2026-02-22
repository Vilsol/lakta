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
//
// Modes:
//   - stmt (default): wrap code in func main() { ... }
//   - decl: just package + imports (for type/func declarations)
//   - skip: extract but do not compile (documented as intentionally incomplete)

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
	code    string
}

func checkFile(path, tmpDir string) error {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	snippets := extractSnippets(path, bufio.NewScanner(f))

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
				mode, imports, ok := parseCompileAnnotation(strings.TrimSpace(info))

				if ok {
					current = &snippet{
						srcFile: path,
						srcLine: lineNum,
						mode:    mode,
						imports: imports,
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
// Returns mode, imports, and whether the compile annotation was present.
func parseCompileAnnotation(info string) (string, []string, bool) {
	parts := strings.Fields(info)
	compileIdx := -1

	for i, p := range parts {
		if p == "compile" || strings.HasPrefix(p, "compile=") {
			compileIdx = i
			break
		}
	}

	if compileIdx < 0 {
		return "", nil, false
	}

	compile := parts[compileIdx]

	var mode string

	if compile == "compile" {
		mode = "stmt"
	} else {
		mode = strings.TrimPrefix(compile, "compile=")
	}

	var imports []string

	for _, p := range parts[compileIdx+1:] {
		if raw, found := strings.CutPrefix(p, "imports="); found {
			raw = strings.Trim(raw, `"`)

			for imp := range strings.SplitSeq(raw, ",") {
				if imp = strings.TrimSpace(imp); imp != "" {
					imports = append(imports, imp)
				}
			}
		}
	}

	return mode, imports, true
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
