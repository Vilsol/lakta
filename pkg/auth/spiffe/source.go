package spiffe

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/samber/oops"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// Source is an x509svid.Source and x509bundle.Source that is usable before the
// agent answers: until then every lookup fails, which fails TLS handshakes.
type Source struct {
	trustDomain spiffeid.TrustDomain
	inner       atomic.Pointer[workloadapi.X509Source]
}

// TrustDomain returns the configured trust domain.
func (s *Source) TrustDomain() spiffeid.TrustDomain { return s.trustDomain }

// GetX509SVID implements x509svid.Source.
func (s *Source) GetX509SVID() (*x509svid.SVID, error) {
	inner := s.inner.Load()
	if inner == nil {
		return nil, oops.Errorf("spiffe: no SVID received from the Workload API yet")
	}
	svid, err := inner.GetX509SVID()
	return svid, oops.Wrapf(err, "spiffe: get SVID")
}

// GetX509BundleForTrustDomain implements x509bundle.Source.
func (s *Source) GetX509BundleForTrustDomain(td spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	inner := s.inner.Load()
	if inner == nil {
		return nil, oops.Errorf("spiffe: no bundle received from the Workload API yet")
	}
	b, err := inner.GetX509BundleForTrustDomain(td)
	return b, oops.Wrapf(err, "spiffe: get bundle")
}

// Ready fails until the first SVID arrives and while the current one has expired.
func (s *Source) Ready(_ context.Context) error {
	svid, err := s.GetX509SVID()
	if err != nil {
		return err
	}
	if len(svid.Certificates) == 0 || time.Now().After(svid.Certificates[0].NotAfter) {
		return oops.Errorf("spiffe: SVID %s has expired", svid.ID)
	}
	return nil
}

// connect blocks until the agent delivers an SVID or ctx ends, retrying with
// backoff, then keeps the source until ctx ends.
func (s *Source) connect(ctx context.Context, socketPath string, onError func(error)) {
	backoff := time.Second
	for {
		src, err := workloadapi.NewX509Source(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)))
		if err == nil {
			s.inner.Store(src)
			<-ctx.Done()
			_ = src.Close()
			return
		}
		if ctx.Err() != nil {
			return
		}
		onError(err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second) //nolint:mnd // exponential backoff, capped
	}
}
