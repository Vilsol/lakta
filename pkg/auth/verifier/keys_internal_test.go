package verifier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarvinJWendt/testza"
)

func TestLazyKeys_WaiterGivesUpWithItsContext(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	k := newLazyKeys(testIssuer, srv.URL)
	go func() { _, _ = k.fetch(context.Background(), false) }()
	<-started

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := k.get(ctx)
	testza.AssertNotNil(t, err)
	testza.AssertTrue(t, time.Since(start) < time.Second, time.Since(start))
}

func TestLazyKeys_ConcurrentCallersShareOneFetch(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	k := newLazyKeys(testIssuer, srv.URL)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			_, err := k.get(ctx)
			testza.AssertNotNil(t, err)
		})
	}
	wg.Wait()
	testza.AssertEqual(t, int32(1), hits.Load())
}
