package fiberserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/config"
	fiberserver "github.com/Vilsol/lakta/pkg/http/fiber"
	"github.com/Vilsol/lakta/pkg/testkit"
	"github.com/gofiber/fiber/v3"
)

const testServerName = "localhost"

// writeTestCert generates a self-signed cert usable as cert/key and as its own
// CA, writing PEM files to t.TempDir() and returning their paths.
func writeTestCert(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	testza.AssertNil(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: testServerName},
		DNSNames:              []string{testServerName},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
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

func TestResolveTLS_DefaultNil(t *testing.T) {
	t.Parallel()

	c := fiberserver.NewConfig()
	cfg, err := c.ResolveTLS()
	testza.AssertNil(t, err)
	testza.AssertNil(t, cfg)
}

func TestResolveTLS_ExplicitConfigWins(t *testing.T) {
	t.Parallel()

	explicit := &tls.Config{MinVersion: tls.VersionTLS13} //nolint:exhaustruct
	c := fiberserver.NewConfig(fiberserver.WithTLSConfig(explicit))
	cfg, err := c.ResolveTLS()
	testza.AssertNil(t, err)
	testza.AssertEqual(t, explicit, cfg)
}

func TestResolveTLS_FromTLSFiles(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	c := fiberserver.NewConfig()
	c.TLS = config.TLS{CertFile: certPath, KeyFile: keyPath}

	cfg, err := c.ResolveTLS()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, cfg)
}

func TestFiberModule_TLS_ServesHTTPS(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	serverTLS, err := config.TLS{CertFile: certPath, KeyFile: keyPath}.ServerConfig()
	testza.AssertNil(t, err)

	m := fiberserver.NewModule(
		fiberserver.WithHost("127.0.0.1"),
		fiberserver.WithPort(0),
		fiberserver.WithTLSConfig(serverTLS),
		fiberserver.WithRouter(func(app *fiber.App) {
			app.Get("/ping", func(c fiber.Ctx) error {
				return c.SendString("pong")
			})
		}),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)

	clientTLS, err := config.TLS{CAFile: certPath}.ClientConfig()
	testza.AssertNil(t, err)
	clientTLS.ServerName = testServerName

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: clientTLS}, //nolint:exhaustruct
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+addr.String()+"/ping", nil)
	testza.AssertNil(t, err)

	resp, err := client.Do(req)
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	testza.AssertEqual(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	testza.AssertNil(t, err)
	testza.AssertEqual(t, "pong", string(body))
}
