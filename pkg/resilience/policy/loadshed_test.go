package policy_test

import (
	"context"
	"testing"

	"github.com/MarvinJWendt/testza"
	"github.com/Vilsol/lakta/pkg/resilience/policy"
	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

const (
	hedgeDelayKey        = "p.hedge.delay"
	hedgeMaxHedgesKey    = "p.hedge.max_hedges"
	alMinKey             = "p.adaptive_limiter.min"
	alMaxKey             = "p.adaptive_limiter.max"
	alInitialKey         = "p.adaptive_limiter.initial"
	alQueueInitialFactor = "p.adaptive_limiter.queueing.initial_factor"
	alQueueMaxFactor     = "p.adaptive_limiter.queueing.max_factor"
)

func prefixed(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[prefix+k] = v
	}
	return out
}

func TestPolicyBuild_LoadShedValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    map[string]any
		wantErr bool
	}{
		{
			name: "hedge happy path",
			data: map[string]any{hedgeDelayKey: "40ms", hedgeMaxHedgesKey: 2},
		},
		{
			name:    "hedge zero delay rejected",
			data:    map[string]any{hedgeDelayKey: "0s", hedgeMaxHedgesKey: 2},
			wantErr: true,
		},
		{
			name:    "hedge negative max_hedges rejected",
			data:    map[string]any{hedgeDelayKey: "40ms", hedgeMaxHedgesKey: -1},
			wantErr: true,
		},
		{
			name: "adaptive_limiter happy path",
			data: map[string]any{alMinKey: 1, alMaxKey: 200, alInitialKey: 20},
		},
		{
			name: "adaptive_limiter with queueing happy path",
			data: map[string]any{
				alMinKey: 1, alMaxKey: 200, alInitialKey: 20,
				alQueueInitialFactor: 2.0, alQueueMaxFactor: 3.0,
			},
		},
		{
			name:    "adaptive_limiter max zero rejected",
			data:    map[string]any{alMinKey: 0, alMaxKey: 0, alInitialKey: 0},
			wantErr: true,
		},
		{
			name:    "adaptive_limiter min greater than max rejected",
			data:    map[string]any{alMinKey: 5, alMaxKey: 2, alInitialKey: 3},
			wantErr: true,
		},
		{
			name:    "adaptive_limiter initial out of range rejected",
			data:    map[string]any{alMinKey: 1, alMaxKey: 10, alInitialKey: 50},
			wantErr: true,
		},
		{
			name: "adaptive_limiter queueing initial_factor below one rejected",
			data: map[string]any{
				alMinKey: 1, alMaxKey: 10, alInitialKey: 5,
				alQueueInitialFactor: 0.5, alQueueMaxFactor: 3.0,
			},
			wantErr: true,
		},
		{
			name: "adaptive_limiter queueing max_factor below initial_factor rejected",
			data: map[string]any{
				alMinKey: 1, alMaxKey: 10, alInitialKey: 5,
				alQueueInitialFactor: 3.0, alQueueMaxFactor: 2.0,
			},
			wantErr: true,
		},
		{
			name: "bulkhead happy path",
			data: map[string]any{"p.bulkhead.max_concurrent": 500},
		},
		{
			name:    "bulkhead max_concurrent zero rejected",
			data:    map[string]any{"p.bulkhead.max_concurrent": 0},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			k := koanf.New(".")
			testza.AssertNoError(t, k.Load(confmap.Provider(prefixed(tt.data), "."), nil))

			var cfg policy.Config
			testza.AssertNoError(t, cfg.LoadFromKoanf(k, "modules.resilience.policy.default"))

			pc := cfg.Policies["p"]
			_, err := pc.Build()
			if tt.wantErr {
				testza.AssertNotNil(t, err)
			} else {
				testza.AssertNoError(t, err)
			}
		})
	}
}

// TestPolicyBuild_CompositionSheds builds a tight adaptive limiter (max 1)
// wrapping a bulkhead and asserts the excess concurrent executions are rejected
// with an unwrapped adaptivelimiter.ErrExceeded (guards the wrapcheck contract).
func TestPolicyBuild_CompositionSheds(t *testing.T) {
	t.Parallel()

	reg := setup(t, map[string]any{
		prefix + "shed.adaptive_limiter.min":     1,
		prefix + "shed.adaptive_limiter.max":     1,
		prefix + "shed.adaptive_limiter.initial": 1,
		prefix + "shed.bulkhead.max_concurrent":  10,
	})

	const n = 6
	release := make(chan struct{})
	admitted := make(chan struct{}, 1)
	results := make(chan error, n)

	for range n {
		go func() {
			results <- reg.Run(context.Background(), "shed", func(_ context.Context) error {
				admitted <- struct{}{}
				<-release
				return nil
			})
		}()
	}

	<-admitted // one execution holds the single permit

	rejected := 0
	for range n - 1 {
		testza.AssertErrorIs(t, <-results, adaptivelimiter.ErrExceeded)
		rejected++
	}
	testza.AssertEqual(t, n-1, rejected)

	close(release)
	testza.AssertNoError(t, <-results) // the admitted execution completes
}
