package reflectcfg

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//nolint:gochecknoglobals // standard golden-file -update flag idiom
var update = flag.Bool("update", false, "update golden files")

// Repeated test-data tokens named to keep the emitter package goconst-clean.
const (
	catDemo   = "demo"
	typWidget = "widget"
	typRaw    = "raw"
	keyHost   = "host"
	keyPort   = "port"
	inPtrInt  = "*int"
	inMapSS   = "map[string]string"
	inSliceS  = "[]string"
	testID    = "https://vilsol.github.io/lakta/lakta.schema.json"
)

// syntheticOutput exercises every row of the type map plus Passthrough and
// code-only exclusion, so the golden freezes the emitter's full behavior.
func syntheticOutput() Output {
	return Output{Modules: []ModuleDoc{
		{
			Category:    catDemo,
			Type:        typWidget,
			Package:     "github.com/Vilsol/lakta/pkg/demo/widget",
			ConfigPath:  "modules.demo.widget.<name>",
			Description: "demo widget module",
			Fields: []FieldDoc{
				{Key: keyHost, Type: goTypeString, Default: "0.0.0.0", EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__HOST", Description: "bind host"},
				{Key: keyPort, Type: inPtrInt, EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__PORT", Description: "listen port"},
				{Key: "timeout", Type: goTypeDuration, Default: "30s", Required: true, EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__TIMEOUT"},
				{Key: "level", Type: goTypeString, Enum: "debug,info,warn", EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__LEVEL"},
				{Key: "tags", Type: inMapSS, EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__TAGS"},
				{Key: "hosts", Type: inSliceS, EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__HOSTS"},
				{Key: "ratio", Type: goTypeFloat64, EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__RATIO"},
				{Key: "enabled", Type: goTypeBool, EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__ENABLED"},
				{Key: "endpoints", Type: "[]widget.EndpointConfig", EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__ENDPOINTS", Fields: []FieldDoc{
					{Key: keyHost, Type: goTypeString, Required: true, Default: "localhost"},
					{Key: "weight", Type: goTypeInt, Default: "1"},
				}},
				{Key: "routes", Type: "map[string]widget.EndpointConfig", EnvVar: "LAKTA_MODULES__DEMO__WIDGET__<NAME>__ROUTES", Fields: []FieldDoc{
					{Key: keyHost, Type: goTypeString, Required: true},
					{Key: "weight", Type: goTypeInt},
				}},
			},
			CodeOnly: []CodeOnlyDoc{{Option: "WithLogger", Type: "*slog.Logger", Description: "sets the logger"}},
		},
		{
			Category:   catDemo,
			Type:       typRaw,
			Package:    "github.com/Vilsol/lakta/pkg/demo/raw",
			ConfigPath: "modules.demo.raw.<name>",
			Passthrough: &PassthroughDoc{
				TargetType:    "T",
				TargetPackage: "github.com/gofiber/fiber/v3",
				TargetVersion: "v3.3.0",
				DocsURL:       "https://pkg.go.dev/github.com/gofiber/fiber/v3@v3.3.0#Config",
			},
		},
	}}
}

func TestSchemaGolden(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	testza.AssertNoError(t, EncodeSchema(&buf, syntheticOutput(), testID))

	goldenPath := filepath.Join("testdata", "schema_synthetic.golden.json")

	if *update {
		testza.AssertNoError(t, os.WriteFile(goldenPath, buf.Bytes(), 0o600))
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed test fixture path
	testza.AssertNoError(t, err)
	testza.AssertEqual(t, string(want), buf.String())
}

func TestSchemaDraft2020Valid(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	testza.AssertNoError(t, EncodeSchema(&buf, syntheticOutput(), testID))

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(buf.Bytes()))
	testza.AssertNoError(t, err)

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	testza.AssertNoError(t, c.AddResource("mem://lakta.schema.json", doc))

	// Compile validates the document against the Draft 2020-12 meta-schema.
	_, err = c.Compile("mem://lakta.schema.json")
	testza.AssertNoError(t, err)
}

func TestFieldSchemaTypeMap(t *testing.T) {
	t.Parallel()

	testza.AssertEqual(t, jsTypeString, fieldSchema(FieldDoc{Type: goTypeString}).Type)
	testza.AssertEqual(t, jsTypeInteger, fieldSchema(FieldDoc{Type: goTypeInt32}).Type)
	testza.AssertEqual(t, jsTypeNumber, fieldSchema(FieldDoc{Type: goTypeFloat64}).Type)
	testza.AssertEqual(t, jsTypeBoolean, fieldSchema(FieldDoc{Type: goTypeBool}).Type)

	// pointer unwraps to its element type
	testza.AssertEqual(t, jsTypeInteger, fieldSchema(FieldDoc{Type: inPtrInt}).Type)

	dur := fieldSchema(FieldDoc{Type: goTypeDuration})
	testza.AssertEqual(t, jsTypeString, dur.Type)
	testza.AssertEqual(t, durationPattern, dur.Pattern)

	m := fieldSchema(FieldDoc{Type: inMapSS})
	testza.AssertEqual(t, jsTypeObject, m.Type)
	elem, ok := m.AdditionalProperties.(*Schema)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, jsTypeString, elem.Type)

	sl := fieldSchema(FieldDoc{Type: inSliceS})
	testza.AssertEqual(t, jsTypeArray, sl.Type)
	testza.AssertNotNil(t, sl.Items)
	testza.AssertEqual(t, jsTypeString, sl.Items.Type)

	en := fieldSchema(FieldDoc{Type: goTypeString, Enum: "a,b,c"})
	testza.AssertEqual(t, []string{"a", "b", "c"}, en.Enum)
	testza.AssertEqual(t, "", en.Type)

	dd := fieldSchema(FieldDoc{Type: goTypeString, Default: "x", Description: "d"})
	testza.AssertEqual(t, "x", dd.Default)
	testza.AssertEqual(t, "d", dd.Description)
}

func TestDefSchemaRequiredAndPassthrough(t *testing.T) {
	t.Parallel()

	// A non-pointer required field joins `required`; a pointer required field does not.
	def := defSchema(ModuleDoc{Fields: []FieldDoc{
		{Key: "dsn", Type: goTypeString, Required: true},
		{Key: keyPort, Type: inPtrInt, Required: true},
		{Key: keyHost, Type: goTypeString},
	}})
	testza.AssertEqual(t, []string{"dsn"}, def.Required)
	testza.AssertEqual(t, false, def.AdditionalProperties)

	// Passthrough flips additionalProperties:true and uses the docs URL as description.
	pt := defSchema(ModuleDoc{Passthrough: &PassthroughDoc{DocsURL: "https://example.test/#T"}})
	testza.AssertEqual(t, true, pt.AdditionalProperties)
	testza.AssertEqual(t, "https://example.test/#T", pt.Description)
}

func TestDefSchemaExcludesCodeOnly(t *testing.T) {
	t.Parallel()

	def := defSchema(ModuleDoc{
		Fields:   []FieldDoc{{Key: keyHost, Type: goTypeString}},
		CodeOnly: []CodeOnlyDoc{{Option: "WithLogger", Type: "*slog.Logger"}},
	})
	testza.AssertLen(t, def.Properties, 1)
	_, hasHost := def.Properties[keyHost]
	testza.AssertTrue(t, hasHost)
	_, hasLogger := def.Properties["WithLogger"]
	testza.AssertFalse(t, hasLogger)
}

func TestBuildSchemaShape(t *testing.T) {
	t.Parallel()

	s := BuildSchema(syntheticOutput(), testID)

	testza.AssertEqual(t, SchemaDialect, s.Schema)
	testza.AssertEqual(t, testID, s.ID)

	// Instance level uses patternProperties + additionalProperties:false.
	widget := s.Properties["modules"].Properties[catDemo].Properties[typWidget]
	testza.AssertEqual(t, false, widget.AdditionalProperties)
	ref := widget.PatternProperties[instanceNamePattern]
	testza.AssertEqual(t, "#/$defs/demo_widget", ref.Ref)

	// $def exists for each module type.
	_, ok := s.Defs["demo_widget"]
	testza.AssertTrue(t, ok)
	_, ok = s.Defs["demo_raw"]
	testza.AssertTrue(t, ok)
}

func TestFieldSchemaTypedDefaults(t *testing.T) {
	t.Parallel()

	// defaults are emitted as their JSON Schema type, not strings
	testza.AssertEqual(t, any(int64(9090)), fieldSchema(FieldDoc{Type: goTypeInt, Default: "9090"}).Default)
	testza.AssertEqual(t, any(true), fieldSchema(FieldDoc{Type: goTypeBool, Default: "true"}).Default)
	testza.AssertEqual(t, any(0.5), fieldSchema(FieldDoc{Type: goTypeFloat64, Default: "0.5"}).Default)
	testza.AssertEqual(t, any("x"), fieldSchema(FieldDoc{Type: goTypeString, Default: "x"}).Default)
	// duration fields are string-typed in the schema, so the default stays "30s"
	testza.AssertEqual(t, any("30s"), fieldSchema(FieldDoc{Type: goTypeDuration, Default: "30s"}).Default)
	// JSON-rendered collection defaults parse into real arrays
	testza.AssertEqual(t, any([]any{"a", "b"}), fieldSchema(FieldDoc{Type: inSliceS, Default: `["a","b"]`}).Default)
	// unparseable values fall back to the raw string rather than being dropped
	testza.AssertEqual(t, any("abc"), fieldSchema(FieldDoc{Type: goTypeInt, Default: "abc"}).Default)
}
