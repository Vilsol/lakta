package config

import (
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

type configFile struct {
	path   string
	parser koanf.Parser
}

type formatDef struct {
	ext    string
	parser koanf.Parser
}

func getSupportedFormats() []formatDef {
	return []formatDef{
		{".yaml", yaml.Parser()},
		{".yml", yaml.Parser()},
		{".json", json.Parser()},
		{".toml", toml.Parser()},
	}
}

// discoverConfigFiles builds the ordered file cascade koanf loads.
//
// Config precedence, lowest to highest (koanf last-wins merge):
//
//  1. base files     lakta.{yaml,yml,json,toml}   (per ConfigDir, in order)
//  2. profile files  lakta.<Profile>.{yaml,...}   (per ConfigDir, when Profile != "")
//  3. env vars       LAKTA_*                      (loadEnvVars)
//  4. CLI flags       --modules.…=…               (loadCLIFlags)
//
// Init loads files -> env -> flags in that order; the profile overlay only
// extends the file list, so reload() replays it for free and the watcher (which
// watches every discovered file) needs no change.
func (m *Module) discoverConfigFiles() []configFile {
	var files []configFile

	formats := getSupportedFormats()
	for _, dir := range m.config.ConfigDirs {
		for _, format := range formats {
			base := filepath.Join(dir, m.config.ConfigName+format.ext)
			if _, err := os.Stat(base); err == nil {
				files = append(files, configFile{
					path:   base,
					parser: format.parser,
				})
			}

			// Profile overlay: lakta.<profile>.<ext>, appended right after the
			// base so its keys win. A missing overlay is skipped like a missing
			// base file.
			if m.config.Profile != "" {
				overlay := filepath.Join(dir, m.config.ConfigName+"."+m.config.Profile+format.ext)
				if _, err := os.Stat(overlay); err == nil {
					files = append(files, configFile{
						path:   overlay,
						parser: format.parser,
					})
				}
			}
		}
	}

	return files
}

func (m *Module) loadConfigFiles(k *koanf.Koanf) error {
	m.configFiles = m.discoverConfigFiles()

	for _, cf := range m.configFiles {
		if err := k.Load(file.Provider(cf.path), cf.parser); err != nil {
			return oops.Wrapf(err, "failed to load config file: %s", cf.path)
		}
	}

	return nil
}
