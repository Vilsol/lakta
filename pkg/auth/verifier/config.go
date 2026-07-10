package verifier

import (
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
)

// maxClockSkew caps the per-issuer clock_skew; a larger configured value is
// clamped to this at Init.
const maxClockSkew = 5 * time.Minute

// defaultClockSkew is applied when an issuer (or the static key) configures no
// clock_skew.
const defaultClockSkew = time.Minute

// staticAlgorithm is the only algorithm the static-key dev path accepts.
const staticAlgorithm = "HS256"

// defaultScopeClaim is the OAuth scope claim path used when none is configured.
const defaultScopeClaim = "scope"

// defaultStaticProfiles is the fail-safe profile allowlist for static_key when
// none is configured: the dev secret loads only under one of these profiles.
func defaultStaticProfiles() []string {
	return []string{"dev", "local", "test"}
}

// Config is unmarshaled from modules.auth.verifier.<instance>.
type Config struct {
	Name       string         `koanf:"-"`
	Issuers    []IssuerConfig `koanf:"issuers"`
	StaticKey  *StaticKey     `koanf:"static_key"`
	ScopeClaim string         `koanf:"scope_claim"`
	RolesClaim string         `koanf:"roles_claim"`
}

// IssuerConfig is one trusted JWKS/OIDC issuer.
type IssuerConfig struct {
	// Issuer is matched against the token iss exactly (no prefix/substring).
	Issuer string `koanf:"issuer"`
	// Audience MUST be non-empty; the token aud must intersect it.
	Audience []string `koanf:"audience"`
	// JWKSURL is the JWKS endpoint; empty triggers OIDC discovery from Issuer.
	JWKSURL string `koanf:"jwks_url"`
	// Algorithms is a hard allowlist, e.g. [RS256, ES256]; alg:none is rejected.
	Algorithms []string `koanf:"algorithms"`
	// ClockSkew is capped at maxClockSkew.
	ClockSkew time.Duration `koanf:"clock_skew"`
}

// StaticKey is the isolated HS256 dev path, allow-only under Profiles.
type StaticKey struct {
	Algorithm string   `koanf:"algorithm"`
	Secret    string   `koanf:"secret"`
	Profiles  []string `koanf:"profiles"`
}

// Option configures the Module.
type Option func(*Config)

// WithName sets the instance name for this module.
func WithName(name string) Option {
	return func(c *Config) { c.Name = name }
}

// NewDefaultConfig returns default configuration.
func NewDefaultConfig() Config {
	return Config{
		Name:       config.DefaultInstanceName,
		ScopeClaim: defaultScopeClaim,
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
