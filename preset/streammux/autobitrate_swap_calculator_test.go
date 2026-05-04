package streammux

import (
	"context"
	"testing"
	"time"

	testassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

// TestAutoBitRateHandler_SwapCalculator_ResetsTransientSlowdownState pins
// the F4 contract for the upward-recovery wedge: replacing the calculator
// at runtime must clear the transient slowdown state so the first decision
// of the new calculator is not dampened by stale lastBitRateDecreaseTS or
// in-flight resolution-change requests carried over from the prior
// (downward-driving) calculator.
//
// Falsification: drop the lastBitRateDecreaseTS / currentResolutionChangeRequest
// resets from SwapCalculator and the assertions below fail.
func TestAutoBitRateHandler_SwapCalculator_ResetsTransientSlowdownState(t *testing.T) {
	ctx := context.Background()

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  codec.Resolution{Width: 1920, Height: 1080},
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
	}
	cfg := AutoBitRateVideoConfig{
		ResolutionsAndBitRates:  resolutions,
		Calculator:              types.AutoBitrateCalculatorStatic(500_000),
		CheckInterval:           time.Second,
		BitRateIncreaseSlowdown: time.Second,
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: cfg,
	}

	// Pre-load transient slowdown state as if a downward decision had
	// just fired.
	h.lastBitRateDecreaseTS = time.Now()
	h.currentResolutionChangeRequest = &resolutionChangeRequest{
		IsUpgrade: false,
		StartedAt: time.Now(),
		LatestAt:  time.Now(),
	}

	newCalc := types.AutoBitrateCalculatorStatic(10_000_000)
	h.SwapCalculator(ctx, newCalc)

	testassert.Equal(t, AutoBitRateCalculator(newCalc), h.AutoBitRateVideoConfig.Calculator,
		"SwapCalculator must replace the calculator")
	testassert.True(t, h.lastBitRateDecreaseTS.IsZero(),
		"SwapCalculator must clear lastBitRateDecreaseTS so the next upward decision from the new calculator is not slowed down")
	testassert.Nil(t, h.currentResolutionChangeRequest,
		"SwapCalculator must clear currentResolutionChangeRequest so the new calculator is not held back by the prior calculator's in-flight downgrade/upgrade")
}

// TestAutoBitRateHandler_isBitRateIncreaseSlowedDown_BypassesIsCritical
// pins the F5 defense-in-depth contract: a critical (operator-driven)
// upward request must bypass the post-decrease slowdown window. Without
// this bypass, an explicit `Static(highTarget)` raise after a
// `Static(lowTarget)` drive sat behind BitRateIncreaseSlowdown indefinitely
// because each tick re-decreased lastBitRateDecreaseTS via the queue
// dynamics, never letting an upward decision through.
//
// Falsification: revert the `!req.IsCritical` clause in the gate predicate
// and the IsCritical=true sub-test fails because the function still reports
// "slowed down".
func TestAutoBitRateHandler_isBitRateIncreaseSlowedDown_BypassesIsCritical(t *testing.T) {
	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			BitRateIncreaseSlowdown: time.Second,
		},
	}
	h.lastBitRateDecreaseTS = time.Now()

	t.Run("non-critical upward inside slowdown window is gated", func(t *testing.T) {
		gated := h.isBitRateIncreaseSlowedDown(types.Ubps(1_000_000), BitRateChangeRequest{
			BitRate:    types.Ubps(2_000_000),
			IsCritical: false,
		}, time.Now())
		require.True(t, gated,
			"non-critical upward bumps within BitRateIncreaseSlowdown must be slowed down")
	})

	t.Run("critical upward inside slowdown window bypasses gate", func(t *testing.T) {
		gated := h.isBitRateIncreaseSlowedDown(types.Ubps(1_000_000), BitRateChangeRequest{
			BitRate:    types.Ubps(2_000_000),
			IsCritical: true,
		}, time.Now())
		require.False(t, gated,
			"IsCritical=true upward bumps must bypass the post-decrease slowdown window — operator-driven raises are not oscillation")
	})

	t.Run("downward request never slowed down", func(t *testing.T) {
		gated := h.isBitRateIncreaseSlowedDown(types.Ubps(2_000_000), BitRateChangeRequest{
			BitRate:    types.Ubps(1_000_000),
			IsCritical: false,
		}, time.Now())
		require.False(t, gated, "downward requests are never gated by the increase-slowdown")
	})

	t.Run("upward request after slowdown window expires", func(t *testing.T) {
		gated := h.isBitRateIncreaseSlowedDown(types.Ubps(1_000_000), BitRateChangeRequest{
			BitRate:    types.Ubps(2_000_000),
			IsCritical: false,
		}, h.lastBitRateDecreaseTS.Add(2*time.Second))
		require.False(t, gated, "after BitRateIncreaseSlowdown elapses the gate must release")
	})
}
