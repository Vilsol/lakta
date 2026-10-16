package spiffe

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/Vilsol/slox"
	"github.com/hellofresh/health-go/v5"
	"github.com/knadh/koanf/v2"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// Module provides a *Source at modules.auth.spiffe.<instance>.
type Module struct {
	lakta.NamedBase

	config Config
	source *Source
	cancel context.CancelFunc
}

// NewModule creates a new SPIFFE module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{NamedBase: lakta.NewNamedBase(cfg.Name), config: cfg}
}

// ConfigPath returns modules.auth.spiffe.<name>.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryAuth, "spiffe", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error { return m.config.LoadFromKoanf(k, m.ConfigPath()) }

// Source returns the module's SVID source (nil before Init).
func (m *Module) Source() *Source { return m.source }

// Init validates config, provides the *Source and starts connecting to the
// agent in the background.
func (m *Module) Init(ctx context.Context) error {
	if m.config.SocketPath == "" {
		return oops.Errorf("spiffe: socket_path is required")
	}
	td, err := spiffeid.TrustDomainFromString(m.config.TrustDomain)
	if err != nil {
		return oops.Wrapf(err, "spiffe: invalid trust_domain %q", m.config.TrustDomain)
	}
	if profile := activeProfile(ctx); profile.IsProduction() && strings.HasSuffix(td.Name(), ".local") {
		return oops.Errorf("spiffe: trust domain %q is a development domain; refused under production profile %q", td.Name(), profile)
	}

	m.source = &Source{trustDomain: td}
	lakta.ProvideValue(ctx, m.source)

	if h, herr := do.Invoke[*health.Health](lakta.GetInjector(ctx)); herr == nil {
		if rerr := h.Register(health.Config{Name: "spiffe-" + m.config.Name, Check: m.source.Ready}); rerr != nil {
			return oops.Wrapf(rerr, "spiffe: failed to register readiness check")
		}
	}

	bg, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.cancel = cancel
	go m.source.connect(bg, m.config.SocketPath, func(err error) {
		slox.Warn(bg, "spiffe: Workload API not available, retrying", slog.Any("error", err))
	})

	return nil
}

// DescribeConfig returns the config loaded from k without changing the module.
func (m *Module) DescribeConfig(k *koanf.Koanf) (any, error) {
	cfg := m.config
	err := cfg.LoadFromKoanf(k, m.ConfigPath())
	return cfg, err
}

func activeProfile(ctx context.Context) config.Profile {
	if p, err := do.Invoke[config.Profile](lakta.GetInjector(ctx)); err == nil {
		return p
	}
	return config.Profile(os.Getenv("LAKTA_PROFILE"))
}

// Shutdown stops the background connection.
func (m *Module) Shutdown(_ context.Context) error {
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

// Provides returns the types this module registers in DI.
func (m *Module) Provides() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[*Source]()}
}

// Dependencies declares the optional types this module reads in Init.
func (m *Module) Dependencies() ([]reflect.Type, []reflect.Type) {
	return nil, []reflect.Type{
		reflect.TypeFor[*koanf.Koanf](),
		reflect.TypeFor[config.Profile](),
		reflect.TypeFor[*health.Health](),
	}
}
