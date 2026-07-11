package reflectcfg

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// SchemaDialect is the Draft 2020-12 meta-schema URI (the "$schema" value).
	SchemaDialect = "https://json-schema.org/draft/2020-12/schema"
	// durationPattern matches Go duration strings for time.Duration fields.
	durationPattern = "^([0-9]+(ns|us|µs|ms|s|m|h))+$"
	// instanceNamePattern matches config instance keys: modules.<cat>.<type>.<name>.
	instanceNamePattern = "^[A-Za-z0-9_-]+$"
	// schemaIndent is the JSON indentation for the emitted schema.
	schemaIndent = "  "
)

// JSON Schema primitive type names emitted in the "type" keyword.
const (
	jsTypeObject  = "object"
	jsTypeString  = "string"
	jsTypeInteger = "integer"
	jsTypeNumber  = "number"
	jsTypeBoolean = "boolean"
	jsTypeArray   = "array"
)

// Go type-name tokens the type-map switch keys off (as produced by formatType).
// Only the tokens that recur across the package are named; rarer scalar names
// stay inline in the classifier switches below.
const (
	goTypeDuration = "time.Duration"
	goTypeBool     = "bool"
	goTypeString   = "string"
	goTypeInt      = "int"
	goTypeInt32    = "int32"
	goTypeFloat64  = "float64"
)

// Schema is a minimal Draft 2020-12 node — only the keywords the config surface
// needs. AdditionalProperties is `any` because it is either a bool (false at the
// category/type levels, true for Passthrough) or a nested *Schema (for maps).
type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	ID                   string             `json:"$id,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Description          string             `json:"description,omitempty"`
	Default              any                `json:"default,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	PatternProperties    map[string]*Schema `json:"patternProperties,omitempty"`    //nolint:tagliatelle // JSON Schema keyword is camelCase
	AdditionalProperties any                `json:"additionalProperties,omitempty"` //nolint:tagliatelle // JSON Schema keyword is camelCase; value is bool | *Schema
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Defs                 map[string]*Schema `json:"$defs,omitempty"`
}

// BuildSchema assembles the root schema from the doc tree. id is the hosted "$id"
// URL. Category and type levels get fixed known keys (schema ships all built-ins);
// the instance level uses patternProperties + additionalProperties:false.
func BuildSchema(out Output, id string) *Schema {
	categories := map[string]*Schema{}
	defs := map[string]*Schema{}

	for _, m := range out.Modules {
		defKey := m.Category + "_" + m.Type
		defs[defKey] = defSchema(m)

		cat, ok := categories[m.Category]
		if !ok {
			cat = &Schema{Type: jsTypeObject, Properties: map[string]*Schema{}}
			categories[m.Category] = cat
		}

		cat.Properties[m.Type] = &Schema{
			Type: jsTypeObject,
			PatternProperties: map[string]*Schema{
				instanceNamePattern: {Ref: "#/$defs/" + defKey},
			},
			AdditionalProperties: false,
		}
	}

	return &Schema{
		Schema: SchemaDialect,
		ID:     id,
		Type:   jsTypeObject,
		Properties: map[string]*Schema{
			"modules": {
				Type:       jsTypeObject,
				Properties: categories,
			},
		},
		Defs: defs,
	}
}

// EncodeSchema writes BuildSchema's result as indented JSON.
func EncodeSchema(w io.Writer, out Output, id string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", schemaIndent)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(BuildSchema(out, id)); err != nil {
		return fmt.Errorf("failed to encode schema: %w", err)
	}
	return nil
}

// defSchema builds the $def object for one module type ($defs key "<cat>_<type>").
func defSchema(m ModuleDoc) *Schema {
	def := &Schema{Type: jsTypeObject, Properties: map[string]*Schema{}}

	var required []string
	for _, f := range m.Fields {
		def.Properties[f.Key] = fieldSchema(f)
		// *T is optional; a non-pointer required:"true" field joins `required`.
		if f.Required && !strings.HasPrefix(f.Type, "*") {
			required = append(required, f.Key)
		}
	}
	def.Required = required

	// CodeOnly fields are intentionally excluded — not user-settable via config.
	if m.Passthrough != nil {
		def.AdditionalProperties = true // Passthrough[T]: arbitrary extra keys allowed
		def.Description = m.Passthrough.DocsURL
	} else {
		def.AdditionalProperties = false
	}

	return def
}

// fieldSchema is the type-map switch (§5a). It keys off the stringified Type that
// formatType already produced, so pointer/map/slice detection is prefix-based.
func fieldSchema(f FieldDoc) *Schema {
	// Documented sub-fields: a nested struct block, or a collection whose
	// same-package struct elements were recursed into — the []/map[ prefix
	// decides whether the object node describes the field itself, its items,
	// or its map values.
	if len(f.Fields) > 0 {
		obj := objectSchema(f.Fields)
		var s *Schema
		switch {
		case strings.HasPrefix(f.Type, "[]"):
			s = &Schema{Type: jsTypeArray, Items: obj}
		case strings.HasPrefix(f.Type, "map["):
			s = &Schema{Type: jsTypeObject, AdditionalProperties: obj}
		default:
			s = obj
		}
		if f.Description != "" {
			s.Description = f.Description
		}
		return s
	}

	t := strings.TrimPrefix(f.Type, "*") // pointer unwrap; optionality handled in defSchema

	var s *Schema
	switch {
	case f.Enum != "":
		s = &Schema{Enum: strings.Split(f.Enum, ",")}
	case t == goTypeDuration:
		s = &Schema{Type: jsTypeString, Pattern: durationPattern}
	case strings.HasPrefix(t, "map["):
		s = &Schema{Type: jsTypeObject, AdditionalProperties: elemSchema(t)}
	case strings.HasPrefix(t, "[]"):
		s = &Schema{Type: jsTypeArray, Items: elemSchema(t)}
	case t == goTypeBool:
		s = &Schema{Type: jsTypeBoolean}
	case isIntType(t):
		s = &Schema{Type: jsTypeInteger}
	case isFloatType(t):
		s = &Schema{Type: jsTypeNumber}
	default:
		s = &Schema{Type: jsTypeString}
	}

	if f.Default != "" {
		s.Default = typedDefault(s.Type, f.Default)
	}
	if f.Description != "" {
		s.Description = f.Description
	}

	return s
}

// typedDefault converts the doc tree's string default to the schema node's
// JSON type, so editors surface `9090` rather than `"9090"`. Unparseable
// values keep the raw string.
func typedDefault(jsType, raw string) any {
	switch jsType {
	case jsTypeInteger:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n
		}
	case jsTypeNumber:
		if fl, err := strconv.ParseFloat(raw, 64); err == nil {
			return fl
		}
	case jsTypeBoolean:
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	case jsTypeArray, jsTypeObject:
		// scalar-collection defaults are JSON-rendered by defaultValue
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v
		}
	}
	return raw
}

// objectSchema builds the object node for documented struct fields (a nested
// config block or a collection element type).
func objectSchema(fields []FieldDoc) *Schema {
	props := map[string]*Schema{}
	var required []string
	for _, sub := range fields {
		props[sub.Key] = fieldSchema(sub)
		if sub.Required && !strings.HasPrefix(sub.Type, "*") {
			required = append(required, sub.Key)
		}
	}
	obj := &Schema{Type: jsTypeObject, Properties: props, AdditionalProperties: false}
	if len(required) > 0 {
		obj.Required = required
	}
	return obj
}

// elemSchema recurses into the element type of a map[string]X or []X string.
func elemSchema(goType string) *Schema {
	var elem string
	switch {
	case strings.HasPrefix(goType, "map["):
		if _, after, found := strings.Cut(goType, "]"); found {
			elem = after
		}
	case strings.HasPrefix(goType, "[]"):
		elem = strings.TrimPrefix(goType, "[]")
	default:
		elem = goType
	}

	return fieldSchema(FieldDoc{Type: elem})
}

// isIntType classifies the scalar Go integer type names.
func isIntType(t string) bool {
	switch t {
	case goTypeInt, "int8", "int16", goTypeInt32, "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"byte", "rune", "uintptr":
		return true
	}
	return false
}

// isFloatType classifies the scalar Go float type names.
func isFloatType(t string) bool {
	return t == "float32" || t == goTypeFloat64
}
