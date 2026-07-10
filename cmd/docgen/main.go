package main

//go:generate go run ../genmodules -o configs_gen.go

import (
	"flag"
	"fmt"
	"os"

	"github.com/Vilsol/lakta/pkg/reflectcfg"
)

// schemaID is the hosted URL where lakta.schema.json is served (docs site
// `site` + `base`); it is the schema's "$id" and the URL users reference in
// `# yaml-language-server: $schema=`.
const schemaID = "https://vilsol.github.io/lakta/lakta.schema.json"

// exitUsage is the exit code for an unknown -format value (usage error).
const exitUsage = 2

func main() {
	format := flag.String("format", "yaml", "output format: yaml or schema")
	flag.Parse()

	modVersions, err := reflectcfg.ParseGoMod()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse go.mod: %v\n", err)
	}

	out := reflectcfg.Reflect(defaultEntries, modVersions)

	switch *format {
	case "yaml":
		err = reflectcfg.EncodeYAML(os.Stdout, out)
	case "schema":
		err = reflectcfg.EncodeSchema(os.Stdout, out, schemaID)
	default:
		fmt.Fprintf(os.Stderr, "unknown format %q (want yaml|schema)\n", *format)
		os.Exit(exitUsage)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "encode failed: %v\n", err)
		os.Exit(1)
	}
}
