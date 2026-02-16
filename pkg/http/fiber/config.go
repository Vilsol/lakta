package fiberserver

import (
	"maps"
	"net/netip"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/gofiber/fiber/v3"
	"github.com/knadh/koanf/v2"
	"github.com/mitchellh/mapstructure"
	"github.com/samber/oops"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 8080
)

// Router registers routes on a fiber app.
type Router func(app *fiber.App)

// Config represents configuration for HTTP Fiber server [Module]
type Config struct {
	// Instance name
	Name string `koanf:"-"`

	// Host specifies the server's hostname or IP address to bind.
	Host string `koanf:"host"`

	// Port specifies the port number the server listens on.
	Port uint16 `koanf:"port"`

	// HealthPath defines the endpoint path for the health check.
	HealthPath string `koanf:"health_path"`

	// Defaults stores the base fiber.Config values that can be overridden by Raw or koanf configurations.
	Defaults *fiber.Config `code_only:"WithDefaults" koanf:"-"`

	// Routers defines a list of Router functions to configure routes for a Fiber application.
	Routers []Router `code_only:"WithRouter" koanf:"-"`

	// Raw passthrough for fiber.Config fields (app_name, read_timeout, etc.)
	Raw config.Passthrough[fiber.Config] `koanf:",remain"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:    config.DefaultInstanceName,
		Host:    defaultHost,
		Port:    defaultPort,
		Routers: make([]Router, 0),
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	cfg := NewDefaultConfig()
	for _, option := range options {
		option(&cfg)
	}
	return cfg
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
// Preserves existing Raw entries as defaults, letting koanf values take precedence.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	existing := c.Raw
	if err := k.Unmarshal(path, c); err != nil {
		return oops.Wrapf(err, "failed to load config from koanf at path %s", path)
	}
	if len(existing) > 0 {
		merged := make(config.Passthrough[fiber.Config], len(existing)+len(c.Raw))
		maps.Copy(merged, existing)
		maps.Copy(merged, c.Raw)
		c.Raw = merged
	}
	return nil
}

// AddrPort returns the parsed address and port for the server.
func (c *Config) AddrPort() (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return netip.AddrPort{}, oops.Wrapf(err, "failed to parse host address")
	}
	return netip.AddrPortFrom(addr, c.Port), nil
}

// ToFiberConfig returns a fiber.Config with Defaults and Raw fields applied.
func (c *Config) ToFiberConfig() fiber.Config {
	cfg := fiber.Config{}
	if c.Defaults != nil {
		cfg = *c.Defaults
	}
	if len(c.Raw) > 0 {
		decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			TagName:          "json", // fiber.Config uses json tags
			Result:           &cfg,
			WeaklyTypedInput: true,
			DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		})
		_ = decoder.Decode(map[string]any(c.Raw))
	}
	return cfg
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithHost sets the host address.
func WithHost(host string) Option {
	return func(m *Config) { m.Host = host }
}

// WithPort sets the port number.
func WithPort(port uint16) Option {
	return func(m *Config) { m.Port = port }
}

// WithHealthPath sets the health check endpoint path.
func WithHealthPath(path string) Option {
	return func(m *Config) { m.HealthPath = path }
}

// WithDefaults sets typed fiber.Config defaults that can be overridden by Raw/koanf values.
func WithDefaults(cfg fiber.Config) Option {
	return func(m *Config) { m.Defaults = &cfg }
}

// WithRouter adds router to the list of routers to be invoked (code-only).
func WithRouter(router Router) Option {
	return func(m *Config) { m.Routers = append(m.Routers, router) }
}
