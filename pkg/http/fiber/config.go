package fiberserver

import (
	"net/netip"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/gofiber/fiber/v3"
	"github.com/knadh/koanf/v2"
	"github.com/mitchellh/mapstructure"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 8080
)

type Router func(app *fiber.App)

// Config represents configuration for HTTP Fiber server [Module]
type Config struct {
	// Instance name (determines config path, cannot come from config file)
	Name string `koanf:"-"`

	// File-configurable fields
	Host       string `koanf:"host"`
	Port       uint16 `koanf:"port"`
	HealthPath string `koanf:"health_path"`

	// Raw passthrough for fiber.Config fields (app_name, read_timeout, etc.)
	Raw map[string]any `koanf:",remain"`

	// Code-only fields
	Routers []Router `koanf:"-"`
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
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return k.Unmarshal(path, c)
}

// AddrPort returns the parsed address and port for the server.
func (c *Config) AddrPort() (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(addr, c.Port), nil
}

// ToFiberConfig returns a fiber.Config with Raw fields applied.
func (c *Config) ToFiberConfig() fiber.Config {
	cfg := fiber.Config{}
	if len(c.Raw) > 0 {
		decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			TagName:          "json", // fiber.Config uses json tags
			Result:           &cfg,
			WeaklyTypedInput: true,
			DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		})
		_ = decoder.Decode(c.Raw)
	}
	return cfg
}

// Option configures the Module.
type Option func(m *Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(m *Config) { m.Name = name }
}

// WithRouter adds router to the list of routers to be invoked (code-only).
func WithRouter(router Router) Option {
	return func(m *Config) { m.Routers = append(m.Routers, router) }
}
