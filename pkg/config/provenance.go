package config

import (
	"sort"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Origin values for a resolved config key.
const (
	OriginFile    = "file"
	OriginEnv     = "env"
	OriginFlag    = "flag"
	OriginDefault = "default"
)

// ProvenanceEntry attributes a config key to the highest layer that set it.
type ProvenanceEntry struct {
	Key    string `json:"key"`
	Origin string `json:"origin"` // file|env|flag|default
	Value  any    `json:"value"`  // pre-redaction; caller redacts before display
}

// ProvenanceSnapshot reconstructs per-key origin by replaying the module's
// layers (files -> env -> flag) into throwaway koanf instances and attributing
// each key to the highest layer containing it; keys present in none are
// "default". koanf has no native per-key origin tracking. Read under RLock.
func (m *Module) ProvenanceSnapshot() []ProvenanceEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fileK := koanf.New(".")
	for _, cf := range m.configFiles {
		_ = fileK.Load(file.Provider(cf.path), cf.parser)
	}

	envK := koanf.New(".")
	prefix := m.config.EnvPrefix
	_ = envK.Load(env.Provider(".", env.Opt{
		Prefix: prefix,
		TransformFunc: func(k, v string) (string, any) {
			return envKeyTransform(prefix, k), v
		},
	}), nil)

	// Only flags explicitly changed on the command line count as the flag layer;
	// posflag pre-populates every key from koanf, so Visit (changed-only) is the
	// correct source.
	flagK := koanf.New(".")
	if m.flagSet != nil {
		m.flagSet.Visit(func(f *pflag.Flag) {
			_ = flagK.Set(f.Name, f.Value.String())
		})
	}

	all := m.koanf.All()
	entries := make([]ProvenanceEntry, 0, len(all))
	for key, val := range all {
		entries = append(entries, ProvenanceEntry{
			Key:    key,
			Origin: originOf(key, fileK, envK, flagK),
			Value:  val,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })

	return entries
}

func originOf(key string, fileK, envK, flagK *koanf.Koanf) string {
	switch {
	case flagK.Exists(key):
		return OriginFlag
	case envK.Exists(key):
		return OriginEnv
	case fileK.Exists(key):
		return OriginFile
	default:
		return OriginDefault
	}
}
