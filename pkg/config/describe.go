package config

import (
	"slices"

	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// systemConfigDir is the deployment overlay mount; describe mode documents the
// service's own configuration, not an environment's.
const systemConfigDir = "/etc/lakta"

// LoadForDescribe loads the base config files and code-supplied values only:
// no profile overlay, environment, CLI flags or system config directory.
func (m *Module) LoadForDescribe() (*koanf.Koanf, error) {
	k := koanf.New(".")
	for _, cf := range discoverFiles(describeConfigDirs(m.config.ConfigDirs), m.config.ConfigName, "") {
		if err := k.Load(file.Provider(cf.path), cf.parser); err != nil {
			return nil, oops.Wrapf(err, "failed to load config file: %s", cf.path)
		}
	}
	if err := m.applyValues(k); err != nil {
		return nil, oops.Wrapf(err, "failed to apply override values")
	}
	return k, nil
}

func describeConfigDirs(dirs []string) []string {
	return slices.DeleteFunc(slices.Clone(dirs), func(d string) bool { return d == systemConfigDir })
}

// DescribeConfig unmarshals the bound struct without validating it, so a
// struct that needs deployment-only values can still be described.
func (m *bindModule[T]) DescribeConfig(k *koanf.Koanf) (any, error) {
	cfg := new(T)
	if err := k.Unmarshal(m.path, cfg); err != nil {
		return nil, oops.Wrapf(err, "failed to unmarshal config at path %q", m.path)
	}
	return cfg, nil
}
