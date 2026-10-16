package lakta

import (
	"context"
	"io"
	"os"

	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	// DescribeEnv switches Run into describe mode: print configuration, start nothing.
	DescribeEnv = "LAKTA_DESCRIBE"

	describeConfig = "config"
)

// ConfigDescriber reports a module's effective configuration from k without
// initializing the module or validating the result.
type ConfigDescriber interface {
	DescribeConfig(k *koanf.Koanf) (any, error)
}

// DescribeLoader loads the configuration describe mode reports: config files
// only, without environment, flags, profile overlay or secret files.
type DescribeLoader interface {
	LoadForDescribe() (*koanf.Koanf, error)
}

// DescribedConfig is one module's configuration at its koanf path.
type DescribedConfig struct {
	Path  string
	Value any
}

// Describer renders described configuration, e.g. reflectcfg.Describe.
type Describer func(w io.Writer, entries []DescribedConfig) error

// WithDescriber sets the renderer used when LAKTA_DESCRIBE=config.
func (r *Runtime) WithDescriber(fn Describer) *Runtime {
	r.describer = fn
	return r
}

func (r *Runtime) describe(_ context.Context, mode string) error {
	if mode != describeConfig {
		return oops.Errorf("unknown %s mode %q (want %q)", DescribeEnv, mode, describeConfig)
	}
	if r.describer == nil {
		return oops.Errorf("describe mode needs Runtime.WithDescriber")
	}

	sorted, _, err := sortModules(r.modules)
	if err != nil {
		return err
	}

	k := koanf.New(".")
	for _, module := range sorted {
		if l, ok := module.(DescribeLoader); ok {
			if k, err = l.LoadForDescribe(); err != nil {
				return oops.Wrapf(err, "failed to load config for describe")
			}
			break
		}
	}

	var entries []DescribedConfig
	for _, module := range sorted {
		d, ok := module.(ConfigDescriber)
		if !ok {
			continue
		}
		c, ok := module.(Configurable)
		if !ok {
			continue
		}
		value, err := d.DescribeConfig(k)
		if err != nil {
			return oops.With("path", c.ConfigPath()).Wrapf(err, "failed to describe config")
		}
		entries = append(entries, DescribedConfig{Path: c.ConfigPath(), Value: value})
	}

	return r.describer(os.Stdout, entries)
}
