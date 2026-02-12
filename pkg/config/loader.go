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

func (m *Module) discoverConfigFiles() []configFile {
	var files []configFile

	formats := getSupportedFormats()
	for _, dir := range m.config.ConfigDirs {
		for _, fmt := range formats {
			path := filepath.Join(dir, m.config.ConfigName+fmt.ext)
			if _, err := os.Stat(path); err == nil {
				files = append(files, configFile{
					path:   path,
					parser: fmt.parser,
				})
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
