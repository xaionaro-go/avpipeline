package orphanretry_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

func TestExponentialPolicySchedulesRetriesAndRetiresAtMaxAttempts(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 17, 0, 0, 0, time.UTC))
	policy, err := orphanretry.ExponentialPolicy[string](orphanretry.ExponentialConfig{
		InitialDelay: time.Second,
		MaxDelay:     5 * time.Second,
		Multiplier:   2,
		MaxAttempts:  3,
		MaxAge:       time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, policy)

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		policy,
		clock.Now,
		recordingRecreate(&calls, errors.New("destination down")),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("video"), "dead-key")

	current := fixedCurrent(id.NoMemberID, true)
	require.Error(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 1)

	clock.Advance(500 * time.Millisecond)
	require.NoError(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 1, "exponential policy must suppress attempts before NextAttemptAt")

	clock.Advance(500 * time.Millisecond)
	require.Error(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 2)

	clock.Advance(2 * time.Second)
	require.Error(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 3)

	clock.Advance(4 * time.Second)
	require.NoError(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 3, "finite max attempts must retire the orphan before a fourth attempt")
}

func TestExponentialPolicyValidationErrorsWrapInvalidConfig(t *testing.T) {
	testCases := []struct {
		name string
		cfg  orphanretry.ExponentialConfig
	}{
		{
			name: "required initial delay",
			cfg: orphanretry.ExponentialConfig{
				Multiplier:  1,
				MaxAttempts: 2,
			},
		},
		{
			name: "multiplier",
			cfg: orphanretry.ExponentialConfig{
				InitialDelay: time.Second,
				Multiplier:   math.NaN(),
				MaxAttempts:  2,
			},
		},
		{
			name: "max delay cap",
			cfg: orphanretry.ExponentialConfig{
				InitialDelay: time.Second,
				MaxDelay:     -time.Second,
				Multiplier:   1,
				MaxAttempts:  2,
			},
		},
		{
			name: "max attempts",
			cfg: orphanretry.ExponentialConfig{
				InitialDelay: time.Second,
				Multiplier:   1,
				MaxAttempts:  1,
			},
		},
		{
			name: "max age",
			cfg: orphanretry.ExponentialConfig{
				InitialDelay: time.Second,
				Multiplier:   1,
				MaxAttempts:  2,
				MaxAge:       -time.Second,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			policy, err := orphanretry.ExponentialPolicy[string](testCase.cfg)
			require.Nil(t, policy)
			require.ErrorIs(t, err, selectorerr.ErrInvalidConfig)
		})
	}
}

func TestExponentialPolicyRetiresAtMaxAge(t *testing.T) {
	ctx := context.Background()
	clock := newManualClock(time.Date(2026, 5, 4, 18, 0, 0, 0, time.UTC))
	policy, err := orphanretry.ExponentialPolicy[string](orphanretry.ExponentialConfig{
		InitialDelay: time.Second,
		Multiplier:   1,
		MaxAttempts:  0,
		MaxAge:       1500 * time.Millisecond,
	})
	require.NoError(t, err)

	var calls []recreateCall[string]
	tracker, err := orphanretry.NewTracker[string](
		policy,
		clock.Now,
		recordingRecreate(&calls, errors.New("destination down")),
	)
	require.NoError(t, err)
	tracker.RecordDemotion(ctx, id.RouteID("audio"), "dead-key")

	current := fixedCurrent(id.NoMemberID, true)
	require.Error(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 1)

	clock.Advance(2 * time.Second)
	require.NoError(t, tracker.Tick(ctx, current))
	require.Len(t, calls, 1, "age retirement must suppress attempts after MaxAge")
}
