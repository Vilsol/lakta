package connectclient

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

const (
	defaultMaxDeadline  = 30 * time.Second
	defaultTokenTimeout = 5 * time.Second
)

// Config represents configuration for the outbound client [Module].
type Config struct {
	// Name is the instance name; one instance per downstream service.
	Name string `koanf:"-"`

	// BaseURL of the downstream service, e.g. https://dbsvc.errs.svc:8080.
	BaseURL string `koanf:"base_url"`

	// MaxDeadline caps every call: the deadline is min(inbound remaining, MaxDeadline).
	MaxDeadline time.Duration `koanf:"max_deadline"`

	// TokenURL, ClientID and ClientSecretFile configure AsService
	// (OAuth2 client credentials). The secret is read from the file at Init.
	TokenURL         string   `koanf:"token_url"`
	ClientID         string   `koanf:"client_id"`
	ClientSecretFile string   `koanf:"client_secret_file"`
	Scopes           []string `koanf:"scopes"`

	// TokenTimeout bounds each token endpoint request.
	TokenTimeout time.Duration `koanf:"token_timeout"`

	// TLSProvider builds the client TLS config during Init (e.g. spiffe.ClientTLS(serverID)).
	TLSProvider func(ctx context.Context) (*tls.Config, error) `code_only:"WithTLSProvider" koanf:"-"`
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{Name: config.DefaultInstanceName, MaxDeadline: defaultMaxDeadline, TokenTimeout: defaultTokenTimeout}
}

// NewConfig returns configuration with provided options based on defaults.
func NewConfig(options ...Option) Config { return config.Apply(NewDefaultConfig(), options...) }

// LoadFromKoanf loads configuration from koanf instance at the given path.
func (c *Config) LoadFromKoanf(k *koanf.Koanf, path string) error {
	return oops.Wrapf(config.UnmarshalKoanf(c, k, path), "failed to unmarshal config")
}

// Option configures the Module.
type Option func(*Config)

// WithName sets the instance name.
func WithName(name string) Option { return func(c *Config) { c.Name = name } }

// WithBaseURL sets the downstream base URL.
func WithBaseURL(url string) Option { return func(c *Config) { c.BaseURL = url } }

// WithMaxDeadline caps call deadlines.
func WithMaxDeadline(d time.Duration) Option { return func(c *Config) { c.MaxDeadline = d } }

// WithTLSProvider sets the client TLS config builder.
func WithTLSProvider(fn func(ctx context.Context) (*tls.Config, error)) Option {
	return func(c *Config) { c.TLSProvider = fn }
}

// WithClientCredentials configures AsService.
func WithClientCredentials(tokenURL, clientID, secretFile string, scopes ...string) Option {
	return func(c *Config) {
		c.TokenURL, c.ClientID, c.ClientSecretFile, c.Scopes = tokenURL, clientID, secretFile, scopes
	}
}

// WithTokenTimeout bounds each token endpoint request.
func WithTokenTimeout(d time.Duration) Option { return func(c *Config) { c.TokenTimeout = d } }
