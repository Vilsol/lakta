// Package spiffetest provides an in-process SPIFFE Workload API and a CA for
// tests: the module under test fetches its SVID from the fake over a Unix
// socket, and tests mint SVIDs for other identities from the same CA.
package spiffetest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/proto/spiffe/workload"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc"
)

const (
	defaultTTL = time.Hour
	caTTL      = 24 * time.Hour
)

// Option configures the fake.
type Option func(*WorkloadAPI)

// Stopped creates the fake without serving; call Start later.
func Stopped() Option { return func(w *WorkloadAPI) { w.startStopped = true } }

// WithTTL sets the lifetime of SVIDs served on the socket.
func WithTTL(d time.Duration) Option { return func(w *WorkloadAPI) { w.ttl = d } }

// WorkloadAPI serves one identity's X.509-SVID on a Unix socket.
type WorkloadAPI struct {
	workload.UnimplementedSpiffeWorkloadAPIServer

	t            *testing.T
	td           spiffeid.TrustDomain
	path         string
	ttl          time.Duration
	startStopped bool
	caCert       *x509.Certificate
	caKey        *ecdsa.PrivateKey
	socket       string

	mu      sync.Mutex
	current *workload.X509SVID
	changed chan struct{}
	server  *grpc.Server
}

// New creates a CA for trustDomain and serves the SVID spiffe://<trustDomain>/<path>.
// The socket lives under a short temp dir (macOS caps Unix socket paths at 104 bytes).
func New(t *testing.T, trustDomain, path string, opts ...Option) *WorkloadAPI {
	t.Helper()

	td := spiffeid.RequireTrustDomainFromString(trustDomain)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("spiffetest: ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "spiffetest CA"},
		URIs:                  []*url.URL{td.ID().URL()},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(caTTL),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("spiffetest: ca cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("spiffetest: parse ca: %v", err)
	}

	dir, err := os.MkdirTemp("", "spf") //nolint:usetesting // t.TempDir paths exceed the 104-byte Unix socket limit on macOS
	if err != nil {
		t.Fatalf("spiffetest: temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	w := &WorkloadAPI{
		t:       t,
		td:      td,
		path:    path,
		ttl:     defaultTTL,
		caCert:  caCert,
		caKey:   caKey,
		socket:  filepath.Join(dir, "agent.sock"),
		changed: make(chan struct{}),
	}
	for _, o := range opts {
		o(w)
	}
	w.current = w.issueProto(w.path, w.ttl)

	if !w.startStopped {
		w.Start()
	}
	t.Cleanup(w.Stop)

	return w
}

// SocketPath is the Workload API address ("unix://...").
func (w *WorkloadAPI) SocketPath() string { return "unix://" + w.socket }

// ID returns spiffe://<trust domain>/<path>.
func (w *WorkloadAPI) ID(path string) spiffeid.ID {
	id, err := spiffeid.FromPath(w.td, "/"+path)
	if err != nil {
		w.t.Fatalf("spiffetest: id: %v", err)
	}
	return id
}

// Start begins serving on the socket.
func (w *WorkloadAPI) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.server != nil {
		return
	}

	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", w.socket)
	if err != nil {
		w.t.Fatalf("spiffetest: listen: %v", err)
	}
	srv := grpc.NewServer()
	workload.RegisterSpiffeWorkloadAPIServer(srv, w)
	w.server = srv
	go func() { _ = srv.Serve(lis) }()
}

// Stop stops serving.
func (w *WorkloadAPI) Stop() {
	w.mu.Lock()
	srv := w.server
	w.server = nil
	w.mu.Unlock()
	if srv != nil {
		srv.Stop()
	}
}

// Rotate issues a fresh SVID and pushes it to connected clients.
func (w *WorkloadAPI) Rotate() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.current = w.issueProto(w.path, w.ttl)
	close(w.changed)
	w.changed = make(chan struct{})
}

// FetchX509SVID streams the current SVID and every rotation.
func (w *WorkloadAPI) FetchX509SVID(_ *workload.X509SVIDRequest, stream grpc.ServerStreamingServer[workload.X509SVIDResponse]) error {
	for {
		w.mu.Lock()
		svid, changed := w.current, w.changed
		w.mu.Unlock()

		if err := stream.Send(&workload.X509SVIDResponse{Svids: []*workload.X509SVID{svid}}); err != nil {
			return err //nolint:wrapcheck // grpc status
		}

		select {
		case <-stream.Context().Done():
			return nil
		case <-changed:
		}
	}
}

// SVID is an identity minted by the fake's CA for use by test clients/servers.
type SVID struct {
	ID          spiffeid.ID
	Certificate tls.Certificate
	roots       *x509.CertPool
}

// Issue mints an SVID for spiffe://<trust domain>/<path>, valid for an hour.
func (w *WorkloadAPI) Issue(path string) *SVID {
	cert, key := w.issue(path, defaultTTL)
	roots := x509.NewCertPool()
	roots.AddCert(w.caCert)
	return &SVID{
		ID:          w.ID(path),
		Certificate: tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert},
		roots:       roots,
	}
}

// ClientTLS presents s and accepts only a server whose SVID is serverID.
func (s *SVID) ClientTLS(serverID spiffeid.ID) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{s.Certificate},
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2", "http/1.1"},
		InsecureSkipVerify: true, //nolint:gosec // verified below against the SPIFFE ID, not a hostname
		VerifyConnection: func(cs tls.ConnectionState) error {
			return verifySPIFFE(cs.PeerCertificates, s.roots, serverID, x509.ExtKeyUsageServerAuth)
		},
	}
}

// ServerTLS presents s and asks for (but does not verify) a client certificate.
func (s *SVID) ServerTLS() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{s.Certificate},
		MinVersion:   tls.VersionTLS12,
		ClientAuth:   tls.RequestClientCert,
		NextProtos:   []string{"h2", "http/1.1"},
	}
}

func (w *WorkloadAPI) issue(path string, ttl time.Duration) (*x509.Certificate, *ecdsa.PrivateKey) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		w.t.Fatalf("spiffetest: leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		URIs:         []*url.URL{w.ID(path).URL()},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(ttl),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, w.caCert, &key.PublicKey, w.caKey)
	if err != nil {
		w.t.Fatalf("spiffetest: leaf cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		w.t.Fatalf("spiffetest: parse leaf: %v", err)
	}
	return cert, key
}

func (w *WorkloadAPI) issueProto(path string, ttl time.Duration) *workload.X509SVID {
	cert, key := w.issue(path, ttl)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		w.t.Fatalf("spiffetest: marshal key: %v", err)
	}
	return &workload.X509SVID{
		SpiffeId:    w.ID(path).String(),
		X509Svid:    cert.Raw,
		X509SvidKey: keyDER,
		Bundle:      w.caCert.Raw,
	}
}

func verifySPIFFE(certs []*x509.Certificate, roots *x509.CertPool, want spiffeid.ID, usage x509.ExtKeyUsage) error {
	if len(certs) == 0 {
		return errNoCertificate
	}
	leaf := certs[0]
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
		return err //nolint:wrapcheck // test helper
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != want.String() {
		return errUnexpectedID
	}
	return nil
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62)) //nolint:mnd // 62-bit serial
	if err != nil {
		panic(err)
	}
	return n
}
