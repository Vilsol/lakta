package main

//go:generate go run ../genmodules -o configs_gen.go

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const (
	yamlIndent = 2
	modulePath = "github.com/Vilsol/lakta"
)

type output struct {
	Modules []moduleDoc `yaml:"modules"`
}

type moduleDoc struct {
	Category    string          `yaml:"category"`
	Type        string          `yaml:"type"`
	Package     string          `yaml:"package"`
	ConfigPath  string          `yaml:"configPath"`
	Description string          `yaml:"description,omitempty"`
	Fields      []fieldDoc      `yaml:"fields,omitempty"`
	Passthrough *passthroughDoc `yaml:"passthrough,omitempty"`
	CodeOnly    []codeOnlyDoc   `yaml:"codeOnly,omitempty"`
}

type fieldDoc struct {
	Key         string `yaml:"key"`
	Type        string `yaml:"type"`
	Default     string `yaml:"default,omitempty"`
	Enum        string `yaml:"enum,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
	EnvVar      string `yaml:"envVar"`
	Description string `yaml:"description,omitempty"`
}

type passthroughDoc struct {
	TargetType    string `yaml:"targetType"`
	TargetPackage string `yaml:"targetPackage"`
	TargetVersion string `yaml:"targetVersion,omitempty"`
	DocsURL       string `yaml:"docsUrl,omitempty"`
}

type codeOnlyDoc struct {
	Option      string `yaml:"option"`
	Type        string `yaml:"type"`
	Description string `yaml:"description,omitempty"`
}

func main() {
	modVersions, err := parseGoMod()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse go.mod: %v\n", err)
	}

	var out output
	for _, cfg := range defaultConfigs {
		doc := processConfig(cfg, modVersions)
		out.Modules = append(out.Modules, doc)
	}

	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode yaml: %v\n", err)
		os.Exit(1)
	}
}

func processConfig(cfg any, modVersions map[string]string) moduleDoc {
	t := reflect.TypeOf(cfg)
	v := reflect.ValueOf(cfg)
	pkgPath := t.PkgPath()

	category, modType := inferCategoryAndType(pkgPath)

	comments := extractComments(pkgPath)

	doc := moduleDoc{
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
				if option == "true" {
					option = f.Name
				}
				doc.CodeOnly = append(doc.CodeOnly, codeOnlyDoc{
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

		fd := fieldDoc{
			Key:         koanfTag,
			Type:        formatType(f.Type),
			Default:     defaultValue(v.FieldByName(f.Name)),
			Enum:        f.Tag.Get("enum"),
			Required:    f.Tag.Get("required") == "true",
			EnvVar:      envVarName(doc.ConfigPath, koanfTag),
			Description: comments.fields[f.Name],
		}
		doc.Fields = append(doc.Fields, fd)
	}

	return doc
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

func extractPassthrough(f reflect.StructField, modVersions map[string]string) *passthroughDoc {
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

		doc := &passthroughDoc{
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
	case "bool", "string",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64",
		"complex64", "complex128",
		"byte", "rune", "error":
		return true
	}
	return false
}

type sourceComments struct {
	structDoc string
	fields    map[string]string
	funcs     map[string]string
}

// extractComments parses the Go source for a package and extracts doc comments
// from the Config struct (type + fields) and WithXxx option functions.
func extractComments(pkgPath string) sourceComments {
	sc := sourceComments{
		fields: make(map[string]string),
		funcs:  make(map[string]string),
	}

	// Resolve package path to filesystem directory
	rel, found := strings.CutPrefix(pkgPath, modulePath+"/")
	if !found {
		return sc
	}
	dir := filepath.Join(".", rel)

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
	for _, spec := range decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Config" {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}

		if decl.Doc != nil {
			sc.structDoc = cleanComment(decl.Doc.Text())
		}

		for _, field := range st.Fields.List {
			if len(field.Names) == 0 || !field.Names[0].IsExported() {
				continue
			}
			name := field.Names[0].Name
			// Prefer doc comment (above), fall back to inline comment
			switch {
			case field.Doc != nil:
				sc.fields[name] = cleanComment(field.Doc.Text())
			case field.Comment != nil:
				sc.fields[name] = cleanComment(field.Comment.Text())
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

// parseGoMod collects dependency versions for passthrough doc links. Post-split
// each pkg lives in its own module, so a dependency like gofiber/fiber/v3 is only
// required by pkg/http/fiber/go.mod, not the root go.mod. We enumerate every module
// in the workspace (go.work) and merge their requires; without a workspace we fall
// back to the single go.mod in the working directory.
func parseGoMod() (map[string]string, error) {
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
