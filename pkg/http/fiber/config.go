package fiberserver

import (
	"context"
	"crypto/tls"
	"net/netip"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/go-viper/mapstructure/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 8080
)

const (
	defaultReadTimeout  = 30 * time.Second
	defaultWriteTimeout = 60 * time.Second
	defaultIdleTimeout  = 120 * time.Second
)

// Router registers routes on a fiber app.
type Router func(app *fiber.App)

// RouterCtx registers routes on a fiber app with context access, so a router
// closure can resolve DI-provided registries (auth, validation, resilience)
// via lakta.Invoke at route-definition time. The ctx carries the injector and
// is passed during the fiber module's Init.
type RouterCtx func(ctx context.Context, app *fiber.App)

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

	// TLS configures file-path based transport security. When unset the server
	// listens in plaintext.
	TLS config.TLS `koanf:"tls"`

	// Defaults stores the base fiber.Config values that can be overridden by Raw or koanf configurations.
	Defaults *fiber.Config `code_only:"WithDefaults" koanf:"-"`

	// TLSConfig overrides TLS with an explicit *tls.Config, e.g. a SPIFFE/SPIRE
	// source via tlsconfig.MTLSServerConfig(...). Takes precedence over TLS.
	TLSConfig *tls.Config `code_only:"WithTLSConfig" koanf:"-"`

	// ErrorHandler sets the fiber.Config-level error handler (code-only). Wire an
	// errfiber.ErrorHandler here to render handler errors as problem+json.
	ErrorHandler *fiber.ErrorHandler `code_only:"WithErrorHandler" koanf:"-"`

	// Routers defines a list of Router functions to configure routes for a Fiber application.
	Routers []Router `code_only:"WithRouter" koanf:"-"`

	// RoutersCtx defines context-aware routers, invoked during Init after Routers.
	RoutersCtx []RouterCtx `code_only:"WithRouterCtx" koanf:"-"`

	// Raw passthrough for fiber.Config fields (app_name, read_timeout, etc.)
	Raw config.Passthrough[fiber.Config] `koanf:",remain"`
}

// NewDefaultConfig returns default configuration
func NewDefaultConfig() Config {
	return Config{
		Name:       config.DefaultInstanceName,
		Host:       defaultHost,
		Port:       defaultPort,
		Routers:    make([]Router, 0),
		RoutersCtx: make([]RouterCtx, 0),
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config {
	return config.Apply(NewDefaultConfig(), options...)
}

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
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
	if c.ErrorHandler != nil {
		cfg.ErrorHandler = *c.ErrorHandler
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = defaultReadTimeout
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = defaultWriteTimeout
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = defaultIdleTimeout
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

// WithRouterCtx adds a context-aware router (code-only). Its closure runs during
// the fiber module's Init after plain Routers; declare the registry it consumes
// as a dep (or ensure the provider module inits earlier) so Invoke resolves.
func WithRouterCtx(router RouterCtx) Option {
	return func(m *Config) { m.RoutersCtx = append(m.RoutersCtx, router) }
}

// WithTLSConfig sets an explicit *tls.Config, overriding TLS file config. Use
// for in-process sources such as SPIFFE/SPIRE (code-only).
func WithTLSConfig(cfg *tls.Config) Option {
	return func(m *Config) { m.TLSConfig = cfg }
}

// WithErrorHandler sets the fiber.Config-level error handler (code-only).
func WithErrorHandler(h fiber.ErrorHandler) Option {
	return func(m *Config) { m.ErrorHandler = &h }
}

// ResolveTLS returns the effective *tls.Config: explicit TLSConfig wins,
// otherwise TLS file paths are loaded, otherwise nil (plaintext).
func (c *Config) ResolveTLS() (*tls.Config, error) {
	if c.TLSConfig != nil {
		return c.TLSConfig, nil
	}

	cfg, err := c.TLS.ServerConfig()

	return cfg, oops.Wrapf(err, "failed to build TLS config")
}
