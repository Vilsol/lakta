// Package reflectcfg reflects module default config structs into a
// documentation tree and emits either the YAML docgen output or a Draft 2020-12
// JSON Schema. It is the reflection source behind lakta's own `docgen`, and the
// public API for downstream services: pair each registered module with its
// default config via FromModule, then Reflect + EncodeYAML/EncodeSchema.
package reflectcfg

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const (
	yamlIndent = 2
	// tagValueTrue is the "true" literal used by the code_only and required tags.
	tagValueTrue = "true"
	// configPathSegments is the segment count of a canonical config path:
	// modules.<category>.<type>.<instance>.
	configPathSegments = 4
)

// Output is the root doc tree.
type Output struct {
	Modules []ModuleDoc `yaml:"modules"`
}

// ModuleDoc describes one module's config surface.
type ModuleDoc struct {
	Category    string          `yaml:"category"`
	Type        string          `yaml:"type"`
	Package     string          `yaml:"package"`
	ConfigPath  string          `yaml:"configPath"`
	Description string          `yaml:"description,omitempty"`
	Fields      []FieldDoc      `yaml:"fields,omitempty"`
	Passthrough *PassthroughDoc `yaml:"passthrough,omitempty"`
	CodeOnly    []CodeOnlyDoc   `yaml:"codeOnly,omitempty"`
}

// FieldDoc is one koanf-settable field. Default/Description populate the schema's
// default/description; Enum/Required/Type drive the type-map switch.
type FieldDoc struct {
	Key         string `yaml:"key"`
	Type        string `yaml:"type"`
	Default     string `yaml:"default,omitempty"`
	Enum        string `yaml:"enum,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
	EnvVar      string `yaml:"envVar,omitempty"`
	Description string `yaml:"description,omitempty"`
	// Fields holds the sub-fields of a nested struct config block (e.g.
	// migrations); empty for scalar fields.
	Fields []FieldDoc `yaml:"fields,omitempty"`
}

// PassthroughDoc captures a Passthrough[T] field's target for the docs URL.
type PassthroughDoc struct {
	TargetType    string `yaml:"targetType"`
	TargetPackage string `yaml:"targetPackage"`
	TargetVersion string `yaml:"targetVersion,omitempty"`
	DocsURL       string `yaml:"docsUrl,omitempty"`
}

// CodeOnlyDoc is a code-only (koanf:"-") option; excluded from the schema.
type CodeOnlyDoc struct {
	Option      string `yaml:"option"`
	Type        string `yaml:"type"`
	Description string `yaml:"description,omitempty"`
}

// Entry pairs one module's default config value with its koanf config path.
type Entry struct {
	// Path is the module's config path, e.g. "modules.grpc.server.default".
	// The instance segment is replaced by "<name>" in the docs; an empty or
	// non-canonical path falls back to package-path inference.
	Path   string
	Config any
}

// FromModule builds an Entry from a module's declared ConfigPath() and its
// default config value — the two halves of the lakta.Configurable contract:
//
//	reflectcfg.FromModule(server.NewModule(), server.NewDefaultConfig())
func FromModule(mod interface{ ConfigPath() string }, cfg any) Entry {
	return Entry{Path: mod.ConfigPath(), Config: cfg}
}

// Reflect walks the registered default config values into the doc tree.
// modVersions maps dependency module paths to versions for passthrough links;
// nil is tolerated (URLs are simply omitted).
func Reflect(entries []Entry, modVersions map[string]string) Output {
	seen := make(map[string]bool, len(entries))
	pkgPaths := make([]string, 0, len(entries))
	for _, e := range entries {
		if p := configType(e.Config).PkgPath(); !seen[p] {
			seen[p] = true
			pkgPaths = append(pkgPaths, p)
		}
	}
	comments := extractComments(pkgPaths)

	var out Output
	for _, e := range entries {
		out.Modules = append(out.Modules, processConfig(e, modVersions, comments))
	}
	return out
}

// configType unwraps a pointer so both Config values and *Config pointers work.
func configType(cfg any) reflect.Type {
	t := reflect.TypeOf(cfg)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// EncodeYAML writes the doc tree as YAML (the current docgen behavior).
func EncodeYAML(w io.Writer, out Output) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("failed to encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("failed to close yaml encoder: %w", err)
	}
	return nil
}

func processConfig(e Entry, modVersions map[string]string, commentsByPkg map[string]sourceComments) ModuleDoc {
	t := reflect.TypeOf(e.Config)
	v := reflect.ValueOf(e.Config)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
		v = v.Elem()
	}
	pkgPath := t.PkgPath()

	category, modType := categoryAndType(e.Path, pkgPath)

	comments := commentsByPkg[pkgPath]

	doc := ModuleDoc{
		Category:    category,
		Type:        modType,
		Package:     pkgPath,
		ConfigPath:  fmt.Sprintf("modules.%s.%s.<name>", category, modType),
		Description: comments.structDoc,
	}

	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}

		koanfTag := f.Tag.Get("koanf")
		codeOnlyTag := f.Tag.Get("code_only")

		// Check for Passthrough field
		if pt := extractPassthrough(f, modVersions); pt != nil {
			doc.Passthrough = pt
			continue
		}

		// Code-only field (koanf:"-" with code_only tag, or koanf:"-" for Name)
		if koanfTag == "-" {
			if codeOnlyTag != "" {
				option := codeOnlyTag
				if option == tagValueTrue {
					option = f.Name
				}
				doc.CodeOnly = append(doc.CodeOnly, CodeOnlyDoc{
					Option:      option,
					Type:        formatType(f.Type),
					Description: comments.funcs[option],
				})
			}
			continue
		}

		// Skip empty koanf tag
		if koanfTag == "" {
			continue
		}

		// Nested struct config block (same package, e.g. migrations): recurse
		// so each sub-field is documented under a nested `fields` tree with
		// dot-notation env vars, instead of an opaque struct blob.
		if f.Type.Kind() == reflect.Struct && f.Type.PkgPath() == pkgPath {
			doc.Fields = append(doc.Fields, FieldDoc{
				Key:         koanfTag,
				Type:        formatType(f.Type),
				Description: comments.fields[f.Name],
				Fields:      structFields(f.Type, v.FieldByName(f.Name), comments, doc.ConfigPath, koanfTag),
			})
			continue
		}

		fd := FieldDoc{
			Key:         koanfTag,
			Type:        formatType(f.Type),
			Default:     defaultValue(v.FieldByName(f.Name)),
			Enum:        f.Tag.Get("enum"),
			Required:    f.Tag.Get("required") == tagValueTrue,
			EnvVar:      envVarName(doc.ConfigPath, koanfTag),
			Description: comments.fields[f.Name],
		}
		doc.Fields = append(doc.Fields, fd)
	}

	return doc
}

// structFields documents the sub-fields of a nested struct config block. keyPath
// is the dotted koanf prefix (e.g. "migrations") used to build each sub-field's
// env var; each returned FieldDoc.Key is the leaf koanf tag so the schema nests
// it under an object. It recurses for further-nested same-package structs.
func structFields(st reflect.Type, sv reflect.Value, comments sourceComments, configPath, keyPath string) []FieldDoc {
	var fields []FieldDoc
	typeName := st.Name()

	for f := range st.Fields() {
		if !f.IsExported() {
			continue
		}
		koanfTag := f.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		fullKey := keyPath + "." + koanfTag

		if f.Type.Kind() == reflect.Struct && f.Type.PkgPath() == st.PkgPath() {
			fields = append(fields, FieldDoc{
				Key:         koanfTag,
				Type:        formatType(f.Type),
				Description: comments.fieldsByType[typeName+"."+f.Name],
				Fields:      structFields(f.Type, sv.FieldByName(f.Name), comments, configPath, fullKey),
			})
			continue
		}

		fields = append(fields, FieldDoc{
			Key:         koanfTag,
			Type:        formatType(f.Type),
			Default:     defaultValue(sv.FieldByName(f.Name)),
			Enum:        f.Tag.Get("enum"),
			Required:    f.Tag.Get("required") == tagValueTrue,
			EnvVar:      envVarName(configPath+"."+keyPath, koanfTag),
			Description: comments.fieldsByType[typeName+"."+f.Name],
		})
	}

	return fields
}

// defaultValue returns a string representation of a field's value,
// or empty string if the value is the zero value for its type.
func defaultValue(v reflect.Value) string {
	if !v.IsValid() || v.IsZero() {
		return ""
	}
	return fmt.Sprintf("%v", v.Interface())
}

// envVarName builds the environment variable name for a config field.
// configPath is e.g. "modules.grpc.server.<name>", key is e.g. "port".
// Result: LAKTA_MODULES__GRPC__SERVER__<NAME>__PORT
//
// Path segments are joined with a double underscore; the snake_case key keeps
// its single underscores, matching the loader's envKeyTransform convention.
func envVarName(configPath, key string) string {
	return "LAKTA_" + strings.ToUpper(strings.ReplaceAll(configPath, ".", "__")+"__"+key)
}

// categoryAndType prefers the module's declared config path
// ("modules.<category>.<type>.<instance>") — the runtime's actual source of
// truth; an empty or non-canonical path falls back to package-path inference.
func categoryAndType(path, pkgPath string) (string, string) {
	if parts := strings.Split(path, "."); len(parts) == configPathSegments && parts[0] == "modules" {
		return parts[1], parts[2]
	}
	return inferCategoryAndType(pkgPath)
}

// inferCategoryAndType extracts category and type from a package path like
// "github.com/Vilsol/lakta/pkg/grpc/server" -> ("grpc", "server")
// "github.com/Vilsol/lakta/pkg/db/drivers/pgx" -> ("db", "pgx")
// "github.com/Vilsol/lakta/pkg/otel" -> ("otel", "otel")
func inferCategoryAndType(pkgPath string) (string, string) {
	_, rest, found := strings.Cut(pkgPath, "/pkg/")
	if !found {
		return pkgPath, pkgPath
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], parts[0]
	}
	return parts[0], parts[len(parts)-1]
}

func extractPassthrough(f reflect.StructField, modVersions map[string]string) *PassthroughDoc {
	// config.Passthrough[T] is a named type with underlying map[string]any.
	// Its full name will be "Passthrough[github.com/gofiber/fiber/v3.Config]"
	typeName := f.Type.Name()
	if !strings.HasPrefix(typeName, "Passthrough[") {
		return nil
	}

	// Extract T's info from the type parameter
	if f.Type.NumMethod() == 0 && f.Type.Kind() == reflect.Map {
		// Get the type argument from the generic instantiation
		// The type name looks like: Passthrough[github.com/gofiber/fiber/v3.Config]
		inner := typeName[len("Passthrough[") : len(typeName)-1]

		lastDot := strings.LastIndex(inner, ".")
		if lastDot == -1 {
			return nil
		}
		targetPkg := inner[:lastDot]
		targetName := inner[lastDot+1:]

		doc := &PassthroughDoc{
			TargetType:    targetName,
			TargetPackage: targetPkg,
		}

		if ver, ok := modVersions[targetPkg]; ok {
			doc.TargetVersion = ver
			doc.DocsURL = fmt.Sprintf("https://pkg.go.dev/%s@%s#%s", targetPkg, ver, targetName)
		}

		return doc
	}

	return nil
}

func formatType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + formatType(t.Elem())
	case reflect.Slice:
		return "[]" + formatType(t.Elem())
	case reflect.Map:
		return "map[" + formatType(t.Key()) + "]" + formatType(t.Elem())
	case reflect.Interface:
		if t.NumMethod() == 0 {
			return "any"
		}
		name := t.Name()
		if name == "" {
			return "interface{}"
		}
		pkg := t.PkgPath()
		if pkg != "" {
			return pkgAlias(pkg) + "." + name
		}
		return name
	default:
		name := t.Name()
		pkg := t.PkgPath()
		if pkg != "" && !isBuiltin(name) {
			return pkgAlias(pkg) + "." + name
		}
		return name
	}
}

// pkgAlias returns a human-friendly package alias, skipping version suffixes
// like "v3" or "v2" to use the actual package name instead.
// For hyphenated names like "health-go", returns the part before the hyphen.
func pkgAlias(pkg string) string {
	parts := strings.Split(pkg, "/")
	last := parts[len(parts)-1]
	if len(parts) >= 2 && len(last) >= 2 && last[0] == 'v' && last[1] >= '0' && last[1] <= '9' {
		last = parts[len(parts)-2]
	}
	if idx := strings.Index(last, "-"); idx > 0 {
		last = last[:idx]
	}
	return last
}

func isBuiltin(name string) bool {
	switch name {
	case goTypeBool, goTypeString,
		"int", "int8", "int16", goTypeInt32, "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", goTypeFloat64,
		"complex64", "complex128",
		"byte", "rune", "error":
		return true
	}
	return false
}

type sourceComments struct {
	structDoc string
	fields    map[string]string
	// fieldsByType keys comments by "TypeName.FieldName" so nested config
	// structs (documented via recursion) resolve their own field docs.
	fieldsByType map[string]string
	funcs        map[string]string
}

// extractComments parses the Go source of each package and extracts doc
// comments from the Config struct (type + fields) and WithXxx option
// functions. Missing packages simply yield empty comments.
func extractComments(pkgPaths []string) map[string]sourceComments {
	all := make(map[string]sourceComments, len(pkgPaths))

	dirs, err := packageDirs(pkgPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve package dirs: %v\n", err)
		return all
	}

	for pkgPath, dir := range dirs {
		all[pkgPath] = parseDirComments(dir)
	}

	return all
}

// packageDirs resolves import paths to source directories with a single
// `go list` invocation, so packages from any module resolve (module cache,
// replace targets, workspace members) regardless of the working directory.
func packageDirs(pkgPaths []string) (map[string]string, error) {
	if len(pkgPaths) == 0 {
		return nil, nil
	}

	// -e keeps one unresolvable package (empty Dir, skipped below) from
	// failing the whole batch.
	args := append([]string{"list", "-e", "-f", "{{.ImportPath}} {{.Dir}}"}, pkgPaths...)
	out, err := exec.CommandContext(context.Background(), "go", args...).Output() //nolint:gosec
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("go list: %w: %s", err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("go list: %w", err)
	}

	dirs := make(map[string]string, len(pkgPaths))
	for line := range strings.Lines(string(out)) {
		if pkgPath, dir, found := strings.Cut(strings.TrimSpace(line), " "); found && dir != "" {
			dirs[pkgPath] = dir
		}
	}

	return dirs, nil
}

// parseDirComments extracts the doc comments from every Go file in dir.
func parseDirComments(dir string) sourceComments {
	sc := sourceComments{
		fields:       make(map[string]string),
		fieldsByType: make(map[string]string),
		funcs:        make(map[string]string),
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments) //nolint:staticcheck
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse %s: %v\n", dir, err)
		return sc
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.GenDecl:
					extractStructComments(d, &sc)
				case *ast.FuncDecl:
					extractFuncComment(d, &sc)
				}
			}
		}
	}

	return sc
}

func extractStructComments(decl *ast.GenDecl, sc *sourceComments) {
	if decl.Tok != token.TYPE {
		return
	}
	if sc.fieldsByType == nil {
		sc.fieldsByType = make(map[string]string)
	}
	for _, spec := range decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}

		isConfig := ts.Name.Name == "Config"
		if isConfig && decl.Doc != nil {
			sc.structDoc = cleanComment(decl.Doc.Text())
		}

		for _, field := range st.Fields.List {
			if len(field.Names) == 0 || !field.Names[0].IsExported() {
				continue
			}
			name := field.Names[0].Name
			// Prefer doc comment (above), fall back to inline comment
			var comment string
			switch {
			case field.Doc != nil:
				comment = cleanComment(field.Doc.Text())
			case field.Comment != nil:
				comment = cleanComment(field.Comment.Text())
			default:
				continue
			}
			sc.fieldsByType[ts.Name.Name+"."+name] = comment
			if isConfig {
				sc.fields[name] = comment
			}
		}
	}
}

func extractFuncComment(decl *ast.FuncDecl, sc *sourceComments) {
	if decl.Doc == nil {
		return
	}
	name := decl.Name.Name
	if !strings.HasPrefix(name, "With") {
		return
	}
	sc.funcs[name] = cleanComment(decl.Doc.Text())
}

// cleanComment trims whitespace and trailing periods from a doc comment.
func cleanComment(s string) string {
	s = strings.TrimSpace(s)
	// Take only the first line for brevity
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	// Strip conventional "FuncName ..." prefix (e.g. "WithPort sets the port number.")
	if idx := strings.Index(s, " "); idx > 0 {
		prefix := s[:idx]
		if strings.HasPrefix(prefix, "With") || prefix == "Config" {
			s = s[idx+1:]
		}
	}
	// Lowercase first letter, trim trailing period
	if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z' {
		s = strings.ToLower(s[:1]) + s[1:]
	}
	s = strings.TrimRight(s, ".")
	return s
}

// ParseGoMod collects dependency versions for passthrough doc links. Post-split
// each pkg lives in its own module, so a dependency like gofiber/fiber/v3 is only
// required by pkg/http/fiber/go.mod, not the root go.mod. We enumerate every module
// in the workspace (go.work) and merge their requires; without a workspace we fall
// back to the single go.mod in the working directory.
func ParseGoMod() (map[string]string, error) {
	modDirs, err := workspaceModuleDirs()
	if err != nil {
		return nil, err
	}

	versions := make(map[string]string)
	for _, dir := range modDirs {
		path := filepath.Join(dir, "go.mod")

		data, readErr := os.ReadFile(path) //nolint:gosec
		if readErr != nil {
			return nil, fmt.Errorf("reading %s: %w", path, readErr)
		}

		f, parseErr := modfile.Parse(path, data, nil)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		for _, req := range f.Require {
			// Select the maximum semver to match what the build resolves via MVS;
			// this is deterministic regardless of go.work ordering.
			if existing, ok := versions[req.Mod.Path]; !ok || semver.Compare(req.Mod.Version, existing) > 0 {
				versions[req.Mod.Path] = req.Mod.Version
			}
		}
	}

	return versions, nil
}

// workspaceModuleDirs returns the directories of every module to scan. It walks up
// from the working directory to find go.work and returns each `use` directory; if no
// workspace is found it returns the working directory alone (single-module fallback).
func workspaceModuleDirs() ([]string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}

	for d := dir; ; {
		workPath := filepath.Join(d, "go.work")

		data, readErr := os.ReadFile(workPath) //nolint:gosec
		if readErr == nil {
			wf, parseErr := modfile.ParseWork(workPath, data, nil)
			if parseErr != nil {
				return nil, fmt.Errorf("parsing %s: %w", workPath, parseErr)
			}

			dirs := make([]string, 0, len(wf.Use))
			for _, use := range wf.Use {
				dirs = append(dirs, filepath.Join(d, filepath.FromSlash(use.Path)))
			}

			return dirs, nil
		}

		parent := filepath.Dir(d)
		if parent == d {
			break
		}

		d = parent
	}

	return []string{dir}, nil
}
