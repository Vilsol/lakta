package main

import (
	"reflect"
	"regexp"
	"testing"
)

var secretLikeKey = regexp.MustCompile(`secret|token|password|passwd|dsn|credential|passphrase|private`)

// Every secret-bearing config field must be tagged secret:"true", or describe
// mode would print its default into the generated docs.
func TestSecretLikeFieldsAreTagged(t *testing.T) {
	t.Parallel()

	for _, e := range defaultEntries {
		auditSecrets(t, e.Path, reflect.TypeOf(e.Config))
	}
}

func auditSecrets(t *testing.T, path string, typ reflect.Type) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for f := range typ.Fields() {
		key := f.Tag.Get("koanf")
		if key == "" || key == "-" {
			continue
		}
		if f.Type.Kind() == reflect.Struct || (f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct) {
			auditSecrets(t, path+"."+key, f.Type)
			continue
		}
		// Keys that look secret but hold a path, URL, type name or duration.
		notSecret := key == "token_type" || key == "token_url" || key == "token_timeout" || key == "client_secret_file"
		if secretLikeKey.MatchString(key) && !notSecret && f.Tag.Get("secret") != "true" {
			t.Errorf("%s.%s looks secret but has no secret:\"true\" tag", path, key)
		}
	}
}
