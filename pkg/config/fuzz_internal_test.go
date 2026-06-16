package config

import (
	"strings"
	"testing"

	"github.com/MarvinJWendt/testza"
)

func FuzzParseFlag(f *testing.F) {
	for _, seed := range []string{"--key=value", "--k=", "noflag", "--noequals", "--=v", "", "----==", "--a=b=c"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, arg string) {
		k, v, ok := parseFlag(arg)
		if ok {
			// A successful parse must round-trip back to the original argument.
			testza.AssertEqual(t, arg, "--"+k+"="+v)
		} else {
			testza.AssertEqual(t, "", k)
			testza.AssertEqual(t, "", v)
		}
	})
}

func FuzzEnvKeyTransform(f *testing.F) {
	seeds := []struct{ prefix, s string }{
		{"APP_", "APP_FOO__BAR"},
		{"", "A__B__C"},
		{"PRE", ""},
		{"X", "X___Y"},
	}
	for _, seed := range seeds {
		f.Add(seed.prefix, seed.s)
	}

	f.Fuzz(func(t *testing.T, prefix, s string) {
		out := envKeyTransform(prefix, s)
		// All "__" separators are collapsed and the result is lowercased.
		testza.AssertFalse(t, strings.Contains(out, "__"))
		testza.AssertEqual(t, strings.ToLower(out), out)
	})
}
