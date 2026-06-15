package config_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
)

// writeTestCert generates a self-signed cert usable as cert/key and as its own
// CA, writing PEM files to t.TempDir() and returning their paths.
func writeTestCert(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	testza.AssertNil(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	testza.AssertNil(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	testza.AssertNil(t, err)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	testza.AssertNil(t, os.WriteFile(certPath, certPEM, 0o600))
	testza.AssertNil(t, os.WriteFile(keyPath, keyPEM, 0o600))

	return certPath, keyPath
}

func TestTLSEnabled(t *testing.T) {
	t.Parallel()

	testza.AssertFalse(t, config.TLS{}.Enabled())
	testza.AssertTrue(t, config.TLS{CertFile: "c"}.Enabled())
	testza.AssertTrue(t, config.TLS{KeyFile: "k"}.Enabled())
	testza.AssertTrue(t, config.TLS{CertFile: "c", KeyFile: "k"}.Enabled())
}

func TestTLSServerConfig_Disabled(t *testing.T) {
	t.Parallel()

	cfg, err := config.TLS{}.ServerConfig()
	testza.AssertNil(t, err)
	testza.AssertNil(t, cfg)
}

func TestTLSServerConfig_Valid(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath}.ServerConfig()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
	testza.AssertEqual(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	testza.AssertEqual(t, 1, len(cfg.Certificates))
}

func TestTLSServerConfig_ClientCAEnablesMutualTLS(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath, ClientCAFile: certPath}.ServerConfig()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
	testza.AssertEqual(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	testza.AssertNotNil(t, cfg.ClientCAs)
}

func TestTLSServerConfig_ClientAuthRequestOverride(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath, ClientAuth: "request"}.ServerConfig()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
	testza.AssertEqual(t, tls.RequestClientCert, cfg.ClientAuth)
}

func TestTLSServerConfig_ClientAuthNoneOverride(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath, ClientCAFile: certPath, ClientAuth: "none"}.ServerConfig()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
	testza.AssertEqual(t, tls.NoClientCert, cfg.ClientAuth)
}

func TestTLSServerConfig_ClientAuthInvalid(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath, ClientAuth: "bogus"}.ServerConfig()
	testza.AssertNotNil(t, err)
	testza.AssertNil(t, cfg)
}

func TestTLSServerConfig_MissingCertFile(t *testing.T) {
	t.Parallel()

	cfg, err := config.TLS{CertFile: "/nonexistent/cert.pem", KeyFile: "/nonexistent/key.pem"}.ServerConfig()
	testza.AssertNotNil(t, err)
	testza.AssertNil(t, cfg)
}

func TestTLSServerConfig_InvalidClientCAFile(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	dir := t.TempDir()
	badCA := filepath.Join(dir, "ca.pem")
	testza.AssertNil(t, os.WriteFile(badCA, []byte("not a pem"), 0o600))

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath, ClientCAFile: badCA}.ServerConfig()
	testza.AssertNotNil(t, err)
	testza.AssertNil(t, cfg)
}

func TestTLSClientConfig_Disabled(t *testing.T) {
	t.Parallel()

	cfg, err := config.TLS{}.ClientConfig()
	testza.AssertNil(t, err)
	testza.AssertNil(t, cfg)
}

func TestTLSClientConfig_CAOnly(t *testing.T) {
	t.Parallel()

	certPath, _ := writeTestCert(t)

	cfg, err := config.TLS{CAFile: certPath}.ClientConfig()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
	testza.AssertEqual(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	testza.AssertNotNil(t, cfg.RootCAs)
	testza.AssertEqual(t, 0, len(cfg.Certificates))
}

func TestTLSClientConfig_WithClientCert(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	cfg, err := config.TLS{CertFile: certPath, KeyFile: keyPath}.ClientConfig()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
	testza.AssertEqual(t, 1, len(cfg.Certificates))
}

func TestTLSClientConfig_BadCertPath(t *testing.T) {
	t.Parallel()

	cfg, err := config.TLS{CertFile: "/nonexistent/cert.pem", KeyFile: "/nonexistent/key.pem"}.ClientConfig()
	testza.AssertNotNil(t, err)
	testza.AssertNil(t, cfg)
}
