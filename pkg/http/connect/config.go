package connect

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/netip"
	"time"

	"connectrpc.com/connect"
	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultHost              = "0.0.0.0"
	defaultPort              = 8080
	defaultReadHeaderTimeout = 10 * time.Second // slowloris guard; ReadTimeout stays off (0) by default
)

// ServiceRegistrar defers handler construction until Init, when the injector is
// in ctx — mirrors grpcclient.ClientRegistrar. It receives the assembled shared
// []connect.HandlerOption (interceptor slots and read limit) and returns a connect constructor's
// (path, handler) pair; connect already yields the concrete types, so no generic
// is needed (unlike WithClient[T]).
type ServiceRegistrar func(ctx context.Context, opts []connect.HandlerOption) (string, http.Handler)

// Config represents configuration for the Connect-RPC server [Module]. One
// net/http+h2c handler serves Connect, gRPC-Web, and gRPC on a single port.
type Config struct {
	// Name is the instance name.
	Name string `koanf:"-"`

	// Host specifies the address for the server to bind to.
	Host string `koanf:"host"`

	// Port represents the port number on which the server listens.
	Port uint16 `koanf:"port"`

	// H2C enables cleartext HTTP/2 (h2c) so gRPC clients work without TLS. When
	// TLS is set, standard HTTP/2-over-TLS is used and H2C is ignored.
	H2C bool `koanf:"h2c"`

	// ReadTimeout bounds reading the entire request incl. body for unary
	// procedures only (a per-request read deadline). Default 0 (off). Streams
	// are never bounded by it.
	ReadTimeout time.Duration `koanf:"read_timeout"`

	// ReadMaxBytes caps each request message (connect ResourceExhausted beyond
	// it) and the whole body of unary procedures. Default 0 (off).
	ReadMaxBytes int `koanf:"read_max_bytes"`

	// MaxHeaderBytes caps request header size (http.Server.MaxHeaderBytes).
	// Default 0 (net/http's 1 MiB).
	MaxHeaderBytes int `koanf:"max_header_bytes"`

	// DefaultDeadline is applied to unary calls whose client sent none.
	// Default 0 (none).
	DefaultDeadline time.Duration `koanf:"default_deadline"`

	// MaxDeadline caps unary client deadlines. Default 0 (no cap).
	MaxDeadline time.Duration `koanf:"max_deadline"`

	// AllowGet permits Connect GET requests (side-effect-free RPCs). Default
	// false: GET is refused with 405.
	AllowGet bool `koanf:"allow_get"`

	// AllowedCallers lists the SPIFFE IDs allowed to call this server when peer
	// auth is active (TLSProvider set or RequirePeerAuth). Empty denies everyone.
	AllowedCallers []string `koanf:"allowed_callers"`

	// TrustedIPCallers lists the SPIFFE IDs whose X-Client-IP header is believed.
	TrustedIPCallers []string `koanf:"trusted_ip_callers"`

	// RequirePeerAuth makes mTLS and a non-empty AllowedCallers mandatory under
	// a production profile (anything but dev/test), failing Init otherwise.
	RequirePeerAuth bool `koanf:"require_peer_auth"`

	// TLSProvider builds the server *tls.Config during Init (e.g. SPIFFE SVIDs).
	// It takes precedence over TLS and disables h2c.
	TLSProvider func(ctx context.Context) (*tls.Config, error) `code_only:"WithTLSProvider" koanf:"-"`

	// ReadHeaderTimeout bounds reading request headers (maps to
	// http.Server.ReadHeaderTimeout). Default 10s slowloris guard.
	ReadHeaderTimeout time.Duration `koanf:"read_header_timeout"`

	// TLS configures file-path based transport security. When unset the server
	// listens in plaintext (h2c if H2C is true).
	TLS config.TLS `koanf:"tls"`

	// Handlers holds directly-registered (path, handler) pairs from WithHandler.
	Handlers map[string]http.Handler `code_only:"WithHandler" koanf:"-"`

	// ServiceRegistrars holds deferred DI-resolved registrars from WithService.
	ServiceRegistrars []ServiceRegistrar `code_only:"WithService" koanf:"-"`

	// ExtraInterceptors holds extra connect interceptors appended after every
	// slot (the innermost position). Named ExtraInterceptors to avoid clashing
	// with the Interceptors() domain method.
	ExtraInterceptors []connect.Interceptor `code_only:"WithInterceptors" koanf:"-"`

	// Slots overrides or fills interceptor slots (see Slot).
	Slots map[Slot]InterceptorFactory `code_only:"WithSlot" koanf:"-"`

	// HTTPMiddlewares wrap the whole handler, first registered outermost. They
	// run before connect reads the request body.
	HTTPMiddlewares []func(http.Handler) http.Handler `code_only:"WithHTTPMiddleware" koanf:"-"`

	// MountedHooks receive the mounted services at the end of Init.
	MountedHooks []MountedHook `code_only:"WithMountedHook" koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name:              config.DefaultInstanceName,
		Host:              defaultHost,
		Port:              defaultPort,
		H2C:               true,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		Handlers:          make(map[string]http.Handler),
	}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config { return config.Apply(NewDefaultConfig(), options...) }

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// AddrPort returns the parsed address and port for the server (copied from grpc/server).
func (c *Config) AddrPort() (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(c.Host)
	if err != nil {
		return netip.AddrPort{}, oops.Wrapf(err, "failed to parse host address")
	}
	return netip.AddrPortFrom(addr, c.Port), nil
}

// ServerTLSConfig builds the *tls.Config for HTTP/2-over-TLS, or nil (plaintext/h2c).
func (c *Config) ServerTLSConfig() (*tls.Config, error) {
	cfg, err := c.TLS.ServerConfig()
	return cfg, oops.Wrapf(err, "failed to build server TLS config")
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

// WithH2C toggles cleartext HTTP/2 (h2c).
func WithH2C(enabled bool) Option {
	return func(m *Config) { m.H2C = enabled }
}

// WithReadTimeout sets http.Server.ReadTimeout (whole request; 0 disables).
func WithReadTimeout(d time.Duration) Option {
	return func(m *Config) { m.ReadTimeout = d }
}

// WithReadHeaderTimeout sets http.Server.ReadHeaderTimeout (slowloris guard).
func WithReadHeaderTimeout(d time.Duration) Option {
	return func(m *Config) { m.ReadHeaderTimeout = d }
}

// WithHandler registers a pre-built (path, handler) pair directly; spreads a
// connect constructor's return: connect.WithHandler(greetv1connect.NewGreetServiceHandler(svc)).
// CAVEAT: the shared interceptor chain is NOT injected here — the handler is
// registered as-is, so the caller owns any options it was built with. Prefer
// WithService (which receives the assembled chain) unless the handler is already
// fully configured.
func WithHandler(path string, handler http.Handler) Option {
	return func(m *Config) {
		if m.Handlers == nil {
			m.Handlers = make(map[string]http.Handler)
		}
		m.Handlers[path] = handler
	}
}

// WithService is the recommended registrar: it defers handler construction to Init
// so it can lakta.Invoke deps, and RECEIVES the assembled shared
// []connect.HandlerOption (interceptor chain) to pass into the connect constructor.
// Not generic — connect constructors already return the concrete (path, handler) pair.
func WithService(fn ServiceRegistrar) Option {
	return func(m *Config) { m.ServiceRegistrars = append(m.ServiceRegistrars, fn) }
}

// WithReadMaxBytes caps request messages and unary bodies.
func WithReadMaxBytes(n int) Option {
	return func(m *Config) { m.ReadMaxBytes = n }
}

// WithMaxHeaderBytes caps request header size.
func WithMaxHeaderBytes(n int) Option {
	return func(m *Config) { m.MaxHeaderBytes = n }
}

// WithDefaultDeadline sets the deadline for unary calls that arrive without one.
func WithDefaultDeadline(d time.Duration) Option {
	return func(m *Config) { m.DefaultDeadline = d }
}

// WithMaxDeadline caps unary client deadlines.
func WithMaxDeadline(d time.Duration) Option {
	return func(m *Config) { m.MaxDeadline = d }
}

// WithAllowGet permits Connect GET requests.
func WithAllowGet(allow bool) Option {
	return func(m *Config) { m.AllowGet = allow }
}

// WithAllowedCallers sets the SPIFFE IDs allowed to call the server.
func WithAllowedCallers(ids ...string) Option {
	return func(m *Config) { m.AllowedCallers = ids }
}

// WithTrustedIPCallers sets the SPIFFE IDs whose X-Client-IP is believed.
func WithTrustedIPCallers(ids ...string) Option {
	return func(m *Config) { m.TrustedIPCallers = ids }
}

// WithRequirePeerAuth makes mTLS and allowed callers mandatory in production.
func WithRequirePeerAuth(require bool) Option {
	return func(m *Config) { m.RequirePeerAuth = require }
}

// WithTLSProvider builds the server TLS config during Init.
func WithTLSProvider(fn func(ctx context.Context) (*tls.Config, error)) Option {
	return func(m *Config) { m.TLSProvider = fn }
}

// WithHTTPMiddleware wraps the server handler; the first registered runs outermost.
func WithHTTPMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(m *Config) { m.HTTPMiddlewares = append(m.HTTPMiddlewares, mw) }
}

// WithInterceptors appends extra connect interceptors after every slot.
func WithInterceptors(interceptors ...connect.Interceptor) Option {
	return func(m *Config) { m.ExtraInterceptors = append(m.ExtraInterceptors, interceptors...) }
}
