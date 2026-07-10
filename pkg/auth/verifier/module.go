package verifier

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/knadh/koanf/v2"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/samber/oops"
)

// profileEnvVar is the active-profile signal that gates the static_key dev path.
const profileEnvVar = "LAKTA_PROFILE"

// minRefreshInterval bounds how often the JWKS cache re-fetches a keyset.
const minRefreshInterval = 15 * time.Minute

// Module provides a *Registry at modules.auth.verifier.<instance> via DI.
type Module struct {
	config   Config
	registry *Registry

	cacheCancel context.CancelFunc
}

// NewModule creates a new auth verifier module.
func NewModule(options ...Option) *Module {
	return &Module{
		config: NewConfig(options...),
	}
}

// ConfigPath returns the koanf path for this module's configuration.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryAuth, "verifier", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error {
	return m.config.LoadFromKoanf(k, m.ConfigPath())
}

// Init builds the Registry from config and provides it into DI.
func (m *Module) Init(ctx context.Context) error {
	state, cancel, err := m.buildState(ctx, m.config)
	if err != nil {
		return err
	}

	m.cacheCancel = cancel
	m.registry = &Registry{}
	m.registry.state.Store(state)

	lakta.ProvideValue(ctx, m.registry)

	return nil
}

// buildState constructs the verifier set (JWKS caches + static key) for cfg.
// The returned cancel stops the background JWKS refresh goroutines.
func (m *Module) buildState(ctx context.Context, cfg Config) (*registryState, context.CancelFunc, error) {
	// Detach from the request scope so background refresh outlives Init, but keep
	// the parent's values (logger, etc.).
	cacheCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	issuers, err := buildIssuers(cacheCtx, cfg.Issuers)
	if err != nil {
		cancel()
		return nil, nil, err
	}

	static, err := m.buildStatic(ctx, cfg.StaticKey)
	if err != nil {
		cancel()
		return nil, nil, err
	}

	return &registryState{
		issuers:    issuers,
		staticKey:  static,
		scopeClaim: cfg.ScopeClaim,
		rolesClaim: cfg.RolesClaim,
	}, cancel, nil
}

// buildIssuers resolves every issuer's JWKS (discovering it when empty) into a
// shared background-refreshing cache and returns the verifier map keyed by iss.
func buildIssuers(ctx context.Context, configs []IssuerConfig) (map[string]*issuerVerifier, error) {
	if len(configs) == 0 {
		return map[string]*issuerVerifier{}, nil
	}

	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, oops.Wrapf(err, "failed to create jwks cache")
	}

	issuers := make(map[string]*issuerVerifier, len(configs))
	for i := range configs {
		ic := configs[i]
		if ic.Issuer == "" {
			return nil, oops.Errorf("auth: issuer must not be empty")
		}
		if len(ic.Audience) == 0 {
			return nil, oops.Errorf("auth: issuer %q audience must be non-empty", ic.Issuer)
		}
		if len(ic.Algorithms) == 0 {
			return nil, oops.Errorf("auth: issuer %q algorithms must be non-empty", ic.Issuer)
		}

		jwksURL := ic.JWKSURL
		if jwksURL == "" {
			jwksURL, err = discoverJWKS(ctx, ic.Issuer)
			if err != nil {
				return nil, err
			}
		}

		if err = cache.Register(ctx, jwksURL, jwk.WithMinInterval(minRefreshInterval)); err != nil {
			return nil, oops.Wrapf(err, "auth: failed to register jwks %q", jwksURL)
		}
		keyset, csErr := cache.CachedSet(jwksURL)
		if csErr != nil {
			return nil, oops.Wrapf(csErr, "auth: failed to build cached keyset for %q", jwksURL)
		}

		skew := ic.ClockSkew
		if skew <= 0 {
			skew = defaultClockSkew
		}
		if skew > maxClockSkew {
			skew = maxClockSkew
		}

		issuers[ic.Issuer] = &issuerVerifier{
			issuer:    ic.Issuer,
			audience:  ic.Audience,
			algs:      ic.Algorithms,
			keyset:    keyset,
			clockSkew: skew,
		}
	}

	return issuers, nil
}

// buildStatic enforces the profile allowlist and constructs the isolated HS256
// verifier, or returns nil when static_key is not configured.
func (m *Module) buildStatic(ctx context.Context, sk *StaticKey) (*staticVerifier, error) {
	if sk == nil {
		return nil, nil
	}

	profiles := sk.Profiles
	if len(profiles) == 0 {
		profiles = defaultStaticProfiles()
	}

	active := os.Getenv(profileEnvVar)
	if active == "" {
		return nil, oops.Errorf("auth: static_key is configured but %s is unset; refusing to load the dev key (fail-safe)", profileEnvVar)
	}
	if !slices.Contains(profiles, active) {
		return nil, oops.Errorf("auth: static_key is not permitted under profile %q (allowed profiles: %v)", active, profiles)
	}

	if sk.Algorithm != "" && sk.Algorithm != staticAlgorithm {
		return nil, oops.Errorf("auth: static_key algorithm must be %s, got %q", staticAlgorithm, sk.Algorithm)
	}
	if sk.Secret == "" {
		return nil, oops.Errorf("auth: static_key secret must not be empty")
	}

	slox.Warn(ctx, "auth: static_key HS256 dev secret is ENABLED — never enable this in production",
		slog.String("profile", active))

	return &staticVerifier{
		secret:    []byte(sk.Secret),
		clockSkew: defaultClockSkew,
	}, nil
}

// discoverJWKS resolves an issuer's jwks_uri via OIDC discovery (discovery-only;
// no token verification happens here).
func discoverJWKS(ctx context.Context, issuer string) (string, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return "", oops.Wrapf(err, "auth: oidc discovery failed for issuer %q", issuer)
	}
	var claims struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&claims); err != nil {
		return "", oops.Wrapf(err, "auth: failed to read oidc discovery document for %q", issuer)
	}
	if claims.JWKSURL == "" {
		return "", oops.Errorf("auth: oidc discovery for %q returned no jwks_uri", issuer)
	}
	return claims.JWKSURL, nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{
		reflect.TypeFor[*Registry](),
	}
}

// Dependencies declares the optional types this module needs from DI before Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
	}
}

// OnReload rebuilds the verifiers only when issuers/audience/static_key (or the
// claim paths) change; JWKS rotation is already automatic via the cache.
func (m *Module) OnReload(k *koanf.Koanf) {
	ctx := context.Background()

	newCfg := m.config
	if err := newCfg.LoadFromKoanf(k, m.ConfigPath()); err != nil {
		slox.Error(ctx, "auth: config reload failed to unmarshal; keeping previous verifiers", slog.Any("error", err))
		return
	}

	if !relevantConfigChanged(m.config, newCfg) {
		return
	}

	state, cancel, err := m.buildState(ctx, newCfg)
	if err != nil {
		slox.Error(ctx, "auth: config reload failed to rebuild verifiers; keeping previous", slog.Any("error", err))
		return
	}

	m.config = newCfg
	m.registry.state.Store(state)
	if m.cacheCancel != nil {
		m.cacheCancel()
	}
	m.cacheCancel = cancel
}

// relevantConfigChanged reports whether a reload touches fields that require
// rebuilding the verifiers.
func relevantConfigChanged(oldCfg, newCfg Config) bool {
	return !reflect.DeepEqual(oldCfg.Issuers, newCfg.Issuers) ||
		!reflect.DeepEqual(oldCfg.StaticKey, newCfg.StaticKey) ||
		oldCfg.ScopeClaim != newCfg.ScopeClaim ||
		oldCfg.RolesClaim != newCfg.RolesClaim
}

// Shutdown stops the background JWKS refresh.
func (m *Module) Shutdown(_ context.Context) error {
	if m.cacheCancel != nil {
		m.cacheCancel()
	}
	return nil
}
