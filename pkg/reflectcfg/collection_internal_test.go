package reflectcfg

import (
	"testing"

	"github.com/MarvinJWendt/testza"
)

const collectionTestPath = "modules.demo.poller.default"

type collectionElemCfg struct {
	Host string `koanf:"host" required:"true"`
	Port int    `koanf:"port"`
}

type collectionCfg struct {
	Endpoints []collectionElemCfg          `koanf:"endpoints"`
	Routes    map[string]collectionElemCfg `koanf:"routes"`
}

type collectionGroupCfg struct {
	Endpoints []collectionElemCfg `koanf:"endpoints"`
}

type collectionNestedCfg struct {
	Group collectionGroupCfg `koanf:"group"`
}

func TestProcessConfigSliceOfStruct(t *testing.T) {
	t.Parallel()

	doc := processConfig(Entry{
		Path:   collectionTestPath,
		Config: collectionCfg{Endpoints: []collectionElemCfg{{Host: "localhost", Port: 9090}}},
	}, nil, nil)

	testza.AssertEqual(t, 2, len(doc.Fields))

	eps := doc.Fields[0]
	testza.AssertEqual(t, "endpoints", eps.Key)
	testza.AssertEqual(t, "[]reflectcfg.collectionElemCfg", eps.Type)
	// the parent field keeps its env var; element fields get none (array
	// elements are not individually env-addressable)
	testza.AssertEqual(t, "LAKTA_MODULES__DEMO__POLLER__<NAME>__ENDPOINTS", eps.EnvVar)
	// the Go-syntax slice blob is dropped in favor of per-field defaults
	testza.AssertEqual(t, "", eps.Default)
	testza.AssertEqual(t, 2, len(eps.Fields))

	host := eps.Fields[0]
	testza.AssertEqual(t, "host", host.Key)
	testza.AssertTrue(t, host.Required)
	// per-field defaults come from the first element of the default slice
	testza.AssertEqual(t, "localhost", host.Default)
	testza.AssertEqual(t, "", host.EnvVar)

	port := eps.Fields[1]
	testza.AssertEqual(t, keyPort, port.Key)
	testza.AssertEqual(t, "9090", port.Default)
	testza.AssertEqual(t, "", port.EnvVar)
}

func TestProcessConfigMapOfStruct(t *testing.T) {
	t.Parallel()

	doc := processConfig(Entry{Path: collectionTestPath, Config: collectionCfg{}}, nil, nil)

	routes := doc.Fields[1]
	testza.AssertEqual(t, "routes", routes.Key)
	testza.AssertEqual(t, "map[string]reflectcfg.collectionElemCfg", routes.Type)
	testza.AssertEqual(t, 2, len(routes.Fields))
	// map defaults are order-dependent, so element fields document zero defaults
	testza.AssertEqual(t, "", routes.Fields[0].Default)
	testza.AssertEqual(t, "", routes.Fields[0].EnvVar)
}

func TestStructFieldsCollectionOfStruct(t *testing.T) {
	t.Parallel()

	doc := processConfig(Entry{
		Path:   collectionTestPath,
		Config: collectionNestedCfg{Group: collectionGroupCfg{Endpoints: []collectionElemCfg{{Port: 8080}}}},
	}, nil, nil)

	group := doc.Fields[0]
	testza.AssertEqual(t, "group", group.Key)
	eps := group.Fields[0]
	testza.AssertEqual(t, "endpoints", eps.Key)
	testza.AssertEqual(t, "LAKTA_MODULES__DEMO__POLLER__<NAME>__GROUP__ENDPOINTS", eps.EnvVar)
	testza.AssertEqual(t, 2, len(eps.Fields))
	testza.AssertEqual(t, "8080", eps.Fields[1].Default)
	testza.AssertEqual(t, "", eps.Fields[1].EnvVar)
}

func TestFieldSchemaCollectionOfStruct(t *testing.T) {
	t.Parallel()

	elemFields := []FieldDoc{
		{Key: keyHost, Type: goTypeString, Required: true, Description: "host addr"},
		{Key: keyPort, Type: goTypeInt, Default: "9090"},
	}

	arr := fieldSchema(FieldDoc{Type: "[]pkg.EndpointConfig", Fields: elemFields, Description: "upstream endpoints"})
	testza.AssertEqual(t, jsTypeArray, arr.Type)
	testza.AssertEqual(t, "upstream endpoints", arr.Description)
	testza.AssertNotNil(t, arr.Items)
	testza.AssertEqual(t, jsTypeObject, arr.Items.Type)
	testza.AssertEqual(t, jsTypeString, arr.Items.Properties[keyHost].Type)
	testza.AssertEqual(t, jsTypeInteger, arr.Items.Properties[keyPort].Type)
	testza.AssertEqual(t, []string{keyHost}, arr.Items.Required)
	testza.AssertEqual(t, false, arr.Items.AdditionalProperties)

	m := fieldSchema(FieldDoc{Type: "map[string]pkg.EndpointConfig", Fields: elemFields})
	testza.AssertEqual(t, jsTypeObject, m.Type)
	obj, ok := m.AdditionalProperties.(*Schema)
	testza.AssertTrue(t, ok)
	testza.AssertEqual(t, jsTypeObject, obj.Type)
	testza.AssertEqual(t, []string{keyHost}, obj.Required)
	testza.AssertEqual(t, false, obj.AdditionalProperties)
}
