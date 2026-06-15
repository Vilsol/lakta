package grpcserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	grpcserver "github.com/Vilsol/lakta/pkg/grpc/server"
	"github.com/Vilsol/lakta/pkg/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
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

func TestServerCredentials_DefaultNil(t *testing.T) {
	t.Parallel()

	c := grpcserver.NewConfig()
	creds, err := c.ServerCredentials()
	testza.AssertNil(t, err)
	testza.AssertNil(t, creds)
}

func TestServerCredentials_ExplicitCredentialsWin(t *testing.T) {
	t.Parallel()

	explicit := insecure.NewCredentials()
	c := grpcserver.NewConfig(grpcserver.WithCredentials(explicit))
	creds, err := c.ServerCredentials()
	testza.AssertNil(t, err)
	testza.AssertEqual(t, explicit, creds)
}

func TestServerCredentials_FromTLSFiles(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	c := grpcserver.NewConfig()
	c.TLS = config.TLS{CertFile: certPath, KeyFile: keyPath}

	creds, err := c.ServerCredentials()
	testza.AssertNil(t, err)
	testza.AssertNotNil(t, creds)
}

func TestGRPCServerModule_TLS_RoundTrip(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	serverTLS, err := config.TLS{CertFile: certPath, KeyFile: keyPath}.ServerConfig()
	testza.AssertNil(t, err)

	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithHealthCheck(true),
		grpcserver.WithCredentials(credentials.NewTLS(serverTLS)),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)

	clientTLS, err := config.TLS{CAFile: certPath}.ClientConfig()
	testza.AssertNil(t, err)
	clientTLS.ServerName = testServerName

	conn, err := grpc.NewClient(addr.String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	testza.AssertNil(t, err)
	testza.AssertEqual(t, healthpb.HealthCheckResponse_NOT_SERVING, resp.GetStatus())
}

func TestGRPCServerModule_TLS_RejectsPlaintextClient(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeTestCert(t)

	serverTLS, err := config.TLS{CertFile: certPath, KeyFile: keyPath}.ServerConfig()
	testza.AssertNil(t, err)

	m := grpcserver.NewModule(
		grpcserver.WithHost("127.0.0.1"),
		grpcserver.WithPort(0),
		grpcserver.WithHealthCheck(true),
		grpcserver.WithCredentials(credentials.NewTLS(serverTLS)),
	)

	testkit.NewRuntimeHarness(t, m)

	addr := testkit.WaitForAddr(t, m)

	conn, err := grpc.NewClient(addr.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	testza.AssertNil(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	testza.AssertNotNil(t, err)
}
