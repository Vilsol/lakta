package connectclient

import (
	"context"
	"crypto/tls"
	"os"
	"strings"

	"github.com/Vilsol/lakta/pkg/config"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Module provides a *Client at modules.http.connect_client.<instance>.
type Module struct {
	lakta.NamedBase

	config Config
	client *Client
}

// NewModule creates an outbound client module.
func NewModule(options ...Option) *Module {
	cfg := NewConfig(options...)
	return &Module{NamedBase: lakta.NewNamedBase(cfg.Name), config: cfg}
}

// ConfigPath returns modules.http.connect_client.<name>.
func (m *Module) ConfigPath() string {
	return config.ModulePath(config.CategoryHTTP, "connect_client", m.config.Name)
}

// LoadConfig loads configuration from koanf.
func (m *Module) LoadConfig(k *koanf.Koanf) error { return m.config.LoadFromKoanf(k, m.ConfigPath()) }

// Client returns the configured client (nil before Init).
func (m *Module) Client() *Client { return m.client }

// Init builds the TLS config and the client-credentials token source.
func (m *Module) Init(ctx context.Context) error {
	if m.config.BaseURL == "" {
		return oops.Errorf("connectclient: base_url is required")
	}

	var tlsConfig *tls.Config
	if m.config.TLSProvider != nil {
		var err error
		if tlsConfig, err = m.config.TLSProvider(ctx); err != nil {
			return oops.Wrapf(err, "connectclient: build TLS config")
		}
	}

	var tokens oauth2.TokenSource
	if m.config.TokenURL != "" {
		secret, err := os.ReadFile(m.config.ClientSecretFile)
		if err != nil {
			return oops.Wrapf(err, "connectclient: read client secret file")
		}
		cc := clientcredentials.Config{
			ClientID:     m.config.ClientID,
			ClientSecret: strings.TrimSpace(string(secret)),
			TokenURL:     m.config.TokenURL,
			Scopes:       m.config.Scopes,
		}
		tokens = cc.TokenSource(context.WithoutCancel(ctx))
	}

	m.client = newClient(tlsConfig, m.config, tokens)
	lakta.ProvideValue(ctx, m.client)

	return nil
}

// Shutdown closes idle connections.
func (m *Module) Shutdown(_ context.Context) error {
	if m.client != nil {
		m.client.httpClient.CloseIdleConnections()
	}
	return nil
}
