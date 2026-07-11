package reflectcfg

import (
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
)

type pointerRetryCfg struct {
	Attempts int           `koanf:"attempts" required:"true"`
	Delay    time.Duration `koanf:"delay"`
}

type pointerBlockCfg struct {
	Retry *pointerRetryCfg `koanf:"retry"`
}

type pointerElemCfg struct {
	Name  string           `koanf:"name"`
	Retry *pointerRetryCfg `koanf:"retry"`
}

type pointerCollectionCfg struct {
	Policies []pointerElemCfg  `koanf:"policies"`
	Backup   []*pointerRetryCfg `koanf:"backup"`
}

func TestProcessConfigPointerStructBlock(t *testing.T) {
	t.Parallel()

	doc := processConfig(Entry{
		Path:   collectionTestPath,
		Config: pointerBlockCfg{Retry: &pointerRetryCfg{Attempts: 3, Delay: 5 * time.Second}},
	}, nil, nil)

	retry := doc.Fields[0]
	testza.AssertEqual(t, "retry", retry.Key)
	testza.AssertEqual(t, "*reflectcfg.pointerRetryCfg", retry.Type)
	testza.AssertEqual(t, 2, len(retry.Fields))

	attempts := retry.Fields[0]
	testza.AssertEqual(t, "attempts", attempts.Key)
	testza.AssertTrue(t, attempts.Required)
	// defaults come from the pointed-to default value
	testza.AssertEqual(t, "3", attempts.Default)
	// pointer blocks are dot-addressable like plain nested structs
	testza.AssertEqual(t, "LAKTA_MODULES__DEMO__POLLER__<NAME>__RETRY__ATTEMPTS", attempts.EnvVar)
	testza.AssertEqual(t, "5s", retry.Fields[1].Default)
}

func TestProcessConfigNilPointerStructBlock(t *testing.T) {
	t.Parallel()

	doc := processConfig(Entry{Path: collectionTestPath, Config: pointerBlockCfg{}}, nil, nil)

	retry := doc.Fields[0]
	// a nil default still documents the block's fields, with zero defaults
	testza.AssertEqual(t, 2, len(retry.Fields))
	testza.AssertEqual(t, "", retry.Fields[0].Default)
}

func TestCollectionElementPointerFields(t *testing.T) {
	t.Parallel()

	doc := processConfig(Entry{
		Path: collectionTestPath,
		Config: pointerCollectionCfg{
			Policies: []pointerElemCfg{{Name: "a", Retry: &pointerRetryCfg{Attempts: 2}}},
			Backup:   []*pointerRetryCfg{{Attempts: 7}},
		},
	}, nil, nil)

	// pointer block inside a collection element recurses, without env vars
	policies := doc.Fields[0]
	retry := policies.Fields[1]
	testza.AssertEqual(t, "retry", retry.Key)
	testza.AssertEqual(t, 2, len(retry.Fields))
	testza.AssertEqual(t, "2", retry.Fields[0].Default)
	testza.AssertEqual(t, "", retry.Fields[0].EnvVar)

	// []*T recurses like []T, defaults from the (dereferenced) first element
	backup := doc.Fields[1]
	testza.AssertEqual(t, "[]*reflectcfg.pointerRetryCfg", backup.Type)
	testza.AssertEqual(t, 2, len(backup.Fields))
	testza.AssertEqual(t, "7", backup.Fields[0].Default)
}

func TestFieldSchemaPointerStructBlock(t *testing.T) {
	t.Parallel()

	s := fieldSchema(FieldDoc{Type: "*policy.RetryConfig", Fields: []FieldDoc{
		{Key: "attempts", Type: goTypeInt, Required: true},
		{Key: "delay", Type: goTypeDuration},
	}})
	testza.AssertEqual(t, jsTypeObject, s.Type)
	testza.AssertEqual(t, jsTypeInteger, s.Properties["attempts"].Type)
	testza.AssertEqual(t, []string{"attempts"}, s.Required)
	testza.AssertEqual(t, false, s.AdditionalProperties)

	// a required pointer block stays exempt from the parent's required list
	def := defSchema(ModuleDoc{Fields: []FieldDoc{
		{Key: "retry", Type: "*policy.RetryConfig", Required: true, Fields: []FieldDoc{{Key: "attempts", Type: goTypeInt}}},
	}})
	testza.AssertEqual(t, 0, len(def.Required))
}
