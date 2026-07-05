package actuator

import (
	"net/netip"
	"strings"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/gofiber/fiber/v3"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultHost     = "127.0.0.1"
	defaultPort     = 6060
	defaultBasePath = "/debug"
)

// ShowValues enum values for the show_values config key.
const (
	ShowNever          = "never"
	ShowAlways         = "always"
	ShowWhenAuthorized = "when_authorized"
)

// Endpoint sub-paths (relative to BasePath). Sensitive ones require auth.
const (
	epModules   = "/modules"
	epStartup   = "/startup"
	epConfig    = "/config"
	epRoutes    = "/routes"
	epInfo      = "/info"
	epHealth    = "/health"
	epDI        = "/di"
	epGoroutine = "/goroutine"
	epVars      = "/vars"
	epLoggers   = "/loggers"
	epPprof     = "/pprof"
)

// EndpointToggles gates the optional endpoint groups. UI is kept as a config
// flag but has no handler this phase (dashboard deferred).
type EndpointToggles struct {
	Pprof   bool `koanf:"pprof"`
	Expvar  bool `koanf:"expvar"`
	UI      bool `koanf:"ui"`
	Loggers bool `koanf:"loggers"`
}

// Config represents configuration for the actuator [Module]. Defaults are
// security-conservative: disabled, loopback-bound, values masked.
type Config struct {
	// Name is the instance name.
	Name string `koanf:"-"`

	// Enabled gates the whole module; when false Init/Start are no-ops. Default false.
	Enabled bool `koanf:"enabled"`

	// Host to bind the private actuator listener. Default 127.0.0.1.
	Host string `koanf:"host"`

	// Port to bind. Default 6060.
	Port uint16 `koanf:"port"`

	// BasePath prefixes every endpoint. Default /debug.
	BasePath string `koanf:"base_path"`

	// ShowValues controls config-value masking: never|always|when_authorized.
	// Default never (matched leaves render as ******).
	ShowValues string `enum:"never,always,when_authorized" koanf:"show_values"`

	// RedactPatterns extends (does not replace) the default key-redaction set.
	RedactPatterns []string `koanf:"redact_patterns"`

	// Endpoints toggles optional endpoint groups. All default true.
	Endpoints EndpointToggles `koanf:"endpoints"`

	// AllowInsecure downgrades the fail-closed security refusals (non-loopback
	// without auth; sensitive endpoints without auth) to a warn. Default false.
	AllowInsecure bool `koanf:"allow_insecure"`

	// Mount, when set, mounts endpoints under the caller's router instead of a
	// private listener (escape hatch, code-only).
	Mount fiber.Router `code_only:"WithMount" koanf:"-"`

	// Auth is caller-supplied middleware gating sensitive (and optionally all)
	// endpoints (code-only). The real JWT verifier is a later phase.
	Auth fiber.Handler `code_only:"WithAuth" koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name:       config.DefaultInstanceName,
		Enabled:    false,
		Host:       defaultHost,
		Port:       defaultPort,
		BasePath:   defaultBasePath,
		ShowValues: ShowNever,
		Endpoints:  EndpointToggles{Pprof: true, Expvar: true, UI: true, Loggers: true},
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config { return config.Apply(NewDefaultConfig(), options...) }

// LoadFromKoanf loads configuration from koanf at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// SecretRedactor compiles the default key patterns plus RedactPatterns into a
// ready Redactor honoring ShowValues.
func (c *Config) SecretRedactor() (*Redactor, error) {
	return NewRedactor(c.RedactPatterns, c.ShowValues)
}

// RequiresAuth reports whether the endpoint (BasePath-relative, e.g.
// "/goroutine") requires auth on any bind. True for goroutine/pprof/vars/loggers.
func (c *Config) RequiresAuth(endpoint string) bool {
	switch {
	case endpoint == epGoroutine, endpoint == epVars, endpoint == epLoggers:
		return true
	case strings.HasPrefix(endpoint, epPprof):
		return true
	default:
		return false
	}
}

// isLoopback reports whether Host resolves to a loopback address (security gate).
func (c *Config) isLoopback() bool {
	if strings.EqualFold(c.Host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}

// addrPort parses the configured Host:Port into a netip.AddrPort.
func (c *Config) addrPort() (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return netip.AddrPort{}, oops.Wrapf(err, "failed to parse host address %q", c.Host)
	}
	return netip.AddrPortFrom(addr, c.Port), nil
}

// Option manipulates Config.
type Option func(m *Config)

// WithName sets the instance name.
func WithName(name string) Option { return func(m *Config) { m.Name = name } }

// WithEnabled toggles the module in code (config normally drives this).
func WithEnabled(enabled bool) Option { return func(m *Config) { m.Enabled = enabled } }

// WithHost sets the bind host in code.
func WithHost(host string) Option { return func(m *Config) { m.Host = host } }

// WithPort sets the bind port in code.
func WithPort(port uint16) Option { return func(m *Config) { m.Port = port } }

// WithMount mounts endpoints under the caller's router instead of a private
// listener (code-only escape hatch).
func WithMount(router fiber.Router) Option { return func(m *Config) { m.Mount = router } }

// WithAuth sets the auth middleware gating sensitive endpoints (code-only).
func WithAuth(h fiber.Handler) Option { return func(m *Config) { m.Auth = h } }
