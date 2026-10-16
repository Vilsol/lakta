package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every configurable module must also be describable, or describe mode
// silently leaves its keys out of the generated docs.
func TestEveryConfigurableModuleIsDescribable(t *testing.T) {
	t.Parallel()

	loads, describes := map[string]bool{}, map[string]bool{}
	root := filepath.Join("..", "..", "pkg")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path) //nolint:gosec // walking our own source tree
		if err != nil {
			return err //nolint:wrapcheck // test helper
		}
		dir := filepath.Dir(path)
		loads[dir] = loads[dir] || strings.Contains(string(src), ") LoadConfig(k *koanf.Koanf) error")
		describes[dir] = describes[dir] || strings.Contains(string(src), ") DescribeConfig(k *koanf.Koanf) (any, error)")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for dir := range loads {
		if loads[dir] && !describes[dir] {
			t.Errorf("%s implements LoadConfig but not DescribeConfig", dir)
		}
	}
}
