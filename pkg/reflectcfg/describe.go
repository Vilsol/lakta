package reflectcfg

import (
	"io"

	"github.com/Vilsol/lakta/pkg/lakta"
)

// Redacted replaces the default of fields tagged secret:"true".
const Redacted = "<redacted>"

// DescribedField is one concrete config key of a described runtime.
type DescribedField struct {
	Key         string `yaml:"key"`
	Type        string `yaml:"type"`
	Default     string `yaml:"default"`
	Env         string `yaml:"env"`
	Description string `yaml:"description"`
	Secret      bool   `yaml:"secret"`
}

type describeOutput struct {
	Entries []DescribedField `yaml:"entries"`
}

// Describe is a lakta.Describer: it writes every config key of entries at its
// exact path, with the env var that sets it and its effective default.
func Describe(w io.Writer, entries []lakta.DescribedConfig) error {
	seen := make(map[string]bool, len(entries))
	pkgPaths := make([]string, 0, len(entries))
	for _, e := range entries {
		if p := configType(e.Value).PkgPath(); !seen[p] {
			seen[p] = true
			pkgPaths = append(pkgPaths, p)
		}
	}
	comments := extractComments(pkgPaths)

	out := describeOutput{Entries: []DescribedField{}}
	for _, e := range entries {
		doc := processConfig(Entry{Path: e.Path, Config: e.Value}, e.Path, nil, comments)
		out.Entries = flatten(out.Entries, e.Path, doc.Fields)
	}

	return encodeYAML(w, out)
}

// flatten turns the doc tree into concrete keys. Nested blocks recurse;
// collections stay one key because their elements are not env-addressable.
func flatten(dst []DescribedField, prefix string, fields []FieldDoc) []DescribedField {
	for _, f := range fields {
		key := prefix + "." + f.Key
		if len(f.Fields) > 0 && f.EnvVar == "" {
			dst = flatten(dst, key, f.Fields)
			continue
		}
		def := f.Default
		if f.Secret {
			def = Redacted
		}
		dst = append(dst, DescribedField{
			Key: key, Type: f.Type, Default: def, Env: f.EnvVar, Description: f.Description, Secret: f.Secret,
		})
	}
	return dst
}
