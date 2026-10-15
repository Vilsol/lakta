package verifier

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/samber/oops"
)

const (
	jwksRefreshInterval = 15 * time.Minute
	jwksInitialBackoff  = time.Second
	jwksMaxBackoff      = 30 * time.Second
	jwksFetchTimeout    = 5 * time.Second
)

// lazyKeys resolves an issuer's JWKS without touching the network at Init:
// the first Verify or the background loop (whichever comes first) discovers
// and fetches it; after that the loop refreshes it and keeps the cached keys
// when a refresh fails.
type lazyKeys struct {
	issuer  string
	refresh time.Duration

	mu       sync.Mutex // guards inflight
	inflight *keysFetch
	jwksURL  string       // written only by the single in-flight fetch
	set      atomic.Value // jwk.Set once loaded
}

// keysFetch is one fetch shared by every caller that arrives while it runs.
type keysFetch struct {
	done chan struct{}
	set  jwk.Set
	err  error
}

func newLazyKeys(issuer, jwksURL string) *lazyKeys {
	return &lazyKeys{issuer: issuer, jwksURL: jwksURL, refresh: jwksRefreshInterval}
}

func (k *lazyKeys) loaded() (jwk.Set, bool) { //nolint:ireturn // jwk.Set is the library's type
	s, ok := k.set.Load().(jwk.Set)
	return s, ok
}

// get returns the cached keys, fetching once synchronously when none are loaded.
func (k *lazyKeys) get(ctx context.Context) (jwk.Set, error) { //nolint:ireturn // jwk.Set is the library's type
	if s, ok := k.loaded(); ok {
		return s, nil
	}
	return k.fetch(ctx, false)
}

// fetch loads the keys, joining a fetch already in flight. Without force it
// returns the cached keys when present. The fetch itself is detached from ctx,
// so a caller giving up does not fail the others; ctx only bounds the wait.
func (k *lazyKeys) fetch(ctx context.Context, force bool) (jwk.Set, error) { //nolint:ireturn // jwk.Set is the library's type
	k.mu.Lock()
	if s, ok := k.loaded(); ok && !force {
		k.mu.Unlock()
		return s, nil
	}
	f := k.inflight
	if f == nil {
		f = &keysFetch{done: make(chan struct{})}
		k.inflight = f
		go k.do(context.WithoutCancel(ctx), f)
	}
	k.mu.Unlock()

	select {
	case <-f.done:
		return f.set, f.err
	case <-ctx.Done():
		return nil, oops.Wrapf(ctx.Err(), "auth: gave up waiting for jwks of %q", k.issuer)
	}
}

func (k *lazyKeys) do(ctx context.Context, f *keysFetch) {
	ctx, cancel := context.WithTimeout(ctx, jwksFetchTimeout)
	defer cancel()

	f.set, f.err = k.load(ctx)
	if f.err == nil {
		k.set.Store(f.set)
	}

	k.mu.Lock()
	k.inflight = nil
	k.mu.Unlock()
	close(f.done)
}

func (k *lazyKeys) load(ctx context.Context) (jwk.Set, error) { //nolint:ireturn // jwk.Set is the library's type
	if k.jwksURL == "" {
		url, err := discoverJWKS(ctx, k.issuer)
		if err != nil {
			return nil, err
		}
		k.jwksURL = url
	}

	set, err := jwk.Fetch(ctx, k.jwksURL)
	if err != nil {
		return nil, oops.Wrapf(err, "auth: failed to fetch jwks %q", k.jwksURL)
	}
	return set, nil
}

// run keeps the keys fresh until ctx is done: backoff while none are loaded,
// the refresh interval afterwards.
func (k *lazyKeys) run(ctx context.Context) {
	backoff := jwksInitialBackoff
	for {
		_, loadedBefore := k.loaded()
		_, err := k.fetch(ctx, loadedBefore)

		wait := k.refresh
		if _, ok := k.loaded(); !ok || (err != nil && !loadedBefore) {
			wait = backoff
			backoff = min(backoff*2, jwksMaxBackoff) //nolint:mnd // exponential backoff
		} else {
			backoff = jwksInitialBackoff
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
