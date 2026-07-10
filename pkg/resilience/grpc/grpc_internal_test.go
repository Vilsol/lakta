package grpc

import (
	"errors"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/failsafe-go/failsafe-go/ratelimiter"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTranslateOverload(t *testing.T) {
	t.Parallel()

	ordinary := errors.New("boom")

	tests := []struct {
		name string
		in   error
		want codes.Code
	}{
		{name: "nil passes through", in: nil, want: codes.OK},
		{name: "bulkhead full to unavailable", in: bulkhead.ErrFull, want: codes.Unavailable},
		{name: "adaptive limiter to unavailable", in: adaptivelimiter.ErrExceeded, want: codes.Unavailable},
		{name: "rate limit to resource exhausted", in: ratelimiter.ErrExceeded, want: codes.ResourceExhausted},
		{name: "ordinary error unchanged", in: ordinary, want: codes.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := translateOverload(tt.in)
			if tt.in == nil {
				testza.AssertNil(t, got)
				return
			}
			testza.AssertEqual(t, tt.want, status.Code(got))
		})
	}

	// Ordinary errors pass through as the identical error value.
	testza.AssertErrorIs(t, translateOverload(ordinary), ordinary)
}
