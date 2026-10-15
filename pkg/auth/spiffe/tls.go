package spiffe

import (
	"context"
	"crypto/tls"
	"crypto/x509"

	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/samber/oops"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
)

// TLSProvider builds a *tls.Config during another module's Init.
type TLSProvider func(ctx context.Context) (*tls.Config, error)

// ServerTLS returns a provider for a server that requires a client SVID from
// the trust domain. The per-service caller allow-list is enforced by the
// server's HTTP layer, so it can answer with a proper error. HTTP/2 stays
// negotiable over ALPN.
func ServerTLS() TLSProvider {
	return func(ctx context.Context) (*tls.Config, error) {
		src, err := lakta.Invoke[*Source](ctx)
		if err != nil {
			return nil, oops.Wrapf(err, "spiffe: no *spiffe.Source in DI")
		}
		cfg := tlsconfig.MTLSServerConfig(src, src, tlsconfig.AuthorizeMemberOf(src.TrustDomain()))
		cfg.MinVersion = tls.VersionTLS12
		cfg.NextProtos = []string{"h2", "http/1.1"}
		return cfg, nil
	}
}

// ClientTLS returns a provider for a client that presents its SVID and accepts
// only the server whose SVID is serverID.
func ClientTLS(serverID string) TLSProvider {
	return func(ctx context.Context) (*tls.Config, error) {
		id, err := spiffeid.FromString(serverID)
		if err != nil {
			return nil, oops.Wrapf(err, "spiffe: invalid server ID %q", serverID)
		}
		src, err := lakta.Invoke[*Source](ctx)
		if err != nil {
			return nil, oops.Wrapf(err, "spiffe: no *spiffe.Source in DI")
		}
		cfg := tlsconfig.MTLSClientConfig(src, src, tlsconfig.AuthorizeID(id))
		cfg.MinVersion = tls.VersionTLS12
		cfg.NextProtos = []string{"h2", "http/1.1"}
		return cfg, nil
	}
}

// PeerID returns the SPIFFE ID (URI SAN) of a verified peer certificate, or "".
func PeerID(cert *x509.Certificate) string {
	if cert == nil || len(cert.URIs) != 1 || cert.URIs[0].Scheme != "spiffe" {
		return ""
	}
	return cert.URIs[0].String()
}
