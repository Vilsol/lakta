package connect

import (
	"bytes"
	"context"
	"crypto/tls"
	stderrors "errors"
	"log"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/Vilsol/lakta/pkg/security"
	"github.com/Vilsol/slox"
)

// ClientIPHeader carries the original client address from a trusted edge.
const ClientIPHeader = "X-Client-IP"

type (
	peerIDKey   struct{}
	clientIPKey struct{}
)

// PeerID returns the caller's verified SPIFFE ID (URI SAN of its client
// certificate), or "" on plaintext connections.
func PeerID(ctx context.Context) string {
	id, _ := ctx.Value(peerIDKey{}).(string)
	return id
}

// ClientIP returns the original client address when a trusted caller supplied it.
func ClientIP(ctx context.Context) (netip.Addr, bool) {
	addr, ok := ctx.Value(clientIPKey{}).(netip.Addr)
	return addr, ok
}

// WithClientIP stores a verified client address, e.g. for outbound propagation tests.
func WithClientIP(ctx context.Context, addr netip.Addr) context.Context {
	return context.WithValue(ctx, clientIPKey{}, addr)
}

// peerAuthActive reports whether callers must present an allow-listed certificate.
func (c *Config) peerAuthActive() bool {
	return c.TLSProvider != nil || c.RequirePeerAuth
}

// peerMiddleware runs before connect reads the body: it identifies the caller
// from its client certificate, enforces allowed_callers when peer auth is
// active (an empty list denies everyone), and accepts X-Client-IP only from
// trusted_ip_callers. The raw header never reaches handlers.
func (c *Config) peerMiddleware(next http.Handler) http.Handler {
	errWriter := connect.NewErrorWriter()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		peer := peerFromTLS(r.TLS)

		if peer != "" {
			ctx = context.WithValue(ctx, peerIDKey{}, peer)
			ctx = slox.With(ctx, slog.String("caller_spiffe_id", peer))
		}

		if c.peerAuthActive() && (peer == "" || !slices.Contains(c.AllowedCallers, peer)) {
			reason := "caller_not_allowed"
			if peer == "" {
				reason = "no_peer_identity"
			}
			id := resolveRequestID(r.Header.Get(RequestIDHeader))
			ctx = withRequestID(ctx, id)
			w.Header().Set(RequestIDHeader, id)
			security.Emit(ctx, security.Event{Reason: reason, Procedure: r.URL.Path, Attrs: []slog.Attr{slog.String("caller_spiffe_id", peer)}})
			_ = errWriter.Write(w, r, connect.NewError(connect.CodePermissionDenied, stderrors.New("caller not allowed")))
			return
		}

		raw := r.Header.Get(ClientIPHeader)
		r.Header.Del(ClientIPHeader)
		if raw != "" && peer != "" && slices.Contains(c.TrustedIPCallers, peer) {
			if addr, err := netip.ParseAddr(raw); err == nil {
				ctx = WithClientIP(ctx, addr)
				ctx = slox.With(ctx, slog.String("client_ip", addr.String()))
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func peerFromTLS(state *tls.ConnectionState) string {
	if state == nil || len(state.PeerCertificates) == 0 {
		return ""
	}
	uris := state.PeerCertificates[0].URIs
	if len(uris) != 1 || uris[0].Scheme != "spiffe" {
		return ""
	}
	return uris[0].String()
}

// handshakeLog turns net/http's "TLS handshake error" lines into security
// events; a failed handshake never reaches the handler chain, so this is the
// only place it is visible. Other server errors are logged as warnings.
type handshakeLog struct {
	ctx context.Context //nolint:containedctx // carries the logger for a log.Logger sink
}

func (h handshakeLog) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if remote, ok := strings.CutPrefix(line, "http: TLS handshake error from "); ok {
		addr, detail, _ := strings.Cut(remote, ": ")
		security.Emit(h.ctx, security.Event{Reason: "tls_handshake", Attrs: []slog.Attr{
			slog.String("remote_addr", addr), slog.String("detail", detail),
		}})
		return len(p), nil
	}
	slox.Warn(h.ctx, "connect server error", slog.String("detail", string(bytes.TrimSpace(p))))
	return len(p), nil
}

func newHandshakeLogger(ctx context.Context) *log.Logger {
	return log.New(handshakeLog{ctx: ctx}, "", 0)
}
