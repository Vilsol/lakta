package config

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"github.com/samber/oops"
)

const tlsMinVersion = tls.VersionTLS12

// TLS holds file-path based TLS settings shared across transport modules.
// All fields are optional; lakta never imports a certificate provider, so any
// source that writes PEM files to disk (cert-manager, Vault Agent, SPIFFE
// helper, static secrets) works through these paths. For dynamic sources that
// cannot be expressed as files (e.g. an in-process SPIFFE X509Source), pass a
// *tls.Config or credentials.TransportCredentials via a module's code-only
// option instead.
type TLS struct {
	// CertFile is the path to the PEM-encoded certificate (server identity, or
	// client identity for mutual TLS).
	CertFile string `koanf:"cert_file"`

	// KeyFile is the path to the PEM-encoded private key for CertFile.
	KeyFile string `koanf:"key_file"`

	// ClientCAFile is the path to a PEM bundle of CAs used by a server to verify
	// client certificates. Setting it enables mutual TLS.
	ClientCAFile string `koanf:"client_ca_file"`

	// CAFile is the path to a PEM bundle of CAs used by a client to verify the
	// server certificate. Leave empty to use the system trust store.
	CAFile string `koanf:"ca_file"`

	// ClientAuth overrides the server's client-certificate policy: "none",
	// "request", "require", "verify", or "require_and_verify". Defaults to
	// "require_and_verify" when ClientCAFile is set, otherwise "none".
	ClientAuth string `koanf:"client_auth"`
}

// Enabled reports whether server-side TLS material (cert + key) is configured.
func (t TLS) Enabled() bool {
	return t.CertFile != "" || t.KeyFile != ""
}

// ServerConfig builds a *tls.Config for a TLS server from the configured file
// paths, or nil when TLS is disabled (no cert/key).
func (t TLS) ServerConfig() (*tls.Config, error) {
	if !t.Enabled() {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to load TLS certificate")
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tlsMinVersion,
	}

	if t.ClientCAFile != "" {
		pool, poolErr := loadCertPool(t.ClientCAFile)
		if poolErr != nil {
			return nil, poolErr
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	if t.ClientAuth != "" {
		mode, modeErr := parseClientAuth(t.ClientAuth)
		if modeErr != nil {
			return nil, modeErr
		}
		cfg.ClientAuth = mode
	}

	return cfg, nil
}

// ClientConfig builds a *tls.Config for a TLS client, presenting a client
// certificate when configured and verifying the server against CAFile. Returns
// nil when no client TLS material is configured.
func (t TLS) ClientConfig() (*tls.Config, error) {
	if t.CertFile == "" && t.KeyFile == "" && t.CAFile == "" {
		return nil, nil
	}

	cfg := &tls.Config{MinVersion: tlsMinVersion}

	if t.CertFile != "" || t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, oops.Wrapf(err, "failed to load TLS certificate")
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if t.CAFile != "" {
		pool, err := loadCertPool(t.CAFile)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}

func loadCertPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path) //nolint:gosec // path is operator-provided TLS config
	if err != nil {
		return nil, oops.Wrapf(err, "failed to read CA file")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, oops.Errorf("no valid certificates found in CA file %s", path)
	}

	return pool, nil
}

func parseClientAuth(mode string) (tls.ClientAuthType, error) {
	switch mode {
	case "none":
		return tls.NoClientCert, nil
	case "request":
		return tls.RequestClientCert, nil
	case "require":
		return tls.RequireAnyClientCert, nil
	case "verify":
		return tls.VerifyClientCertIfGiven, nil
	case "require_and_verify":
		return tls.RequireAndVerifyClientCert, nil
	default:
		return 0, oops.Errorf("invalid client_auth mode %q", mode)
	}
}
