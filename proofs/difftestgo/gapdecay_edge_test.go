// gapdecay_edge_test.go tests specific edge cases designed to trigger Go bugs
// in the QueueSizeGapDecay calculator.  (agent-generated test)
package difftestgo

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// TestGapDecayDivisionByZero tests that outputBR=0 does not panic.
// Go computes: d.QueueSizeMin.Tob().ToS(req.ActualOutputBitrate)
// where ToS does float64(v) / float64(r) * float64(time.Second).
// When r=0, this produces +Inf or NaN — no panic, but potentially
// garbage results.
func TestGapDecayDivisionByZero(t *testing.T) {
	// outputBR = 0 → division by zero in queueDurationOptimal computation.
	// Go: float64(bits)/float64(0) = +Inf, int64(+Inf * ...) is undefined.
	calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: 3 * time.Second,
		QueueSizeMin:         200_000,
		GapDecay:             3 * time.Second,
		InertiaIncrease:      10 * time.Second,
		InertiaDecrease:      2 * time.Second,
		DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
	}

	cfg := &smtypes.AutoBitRateVideoConfig{
		CheckInterval: 1 * time.Second,
	}

	req := smtypes.CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		InputBitrate:          5_000_000,
		ActualOutputBitrate:   0, // division by zero trigger
		QueueDuration:         5 * time.Second,
		QueueSize:             0,
		QueueSizeDerivative:   100,
		Config:                cfg,
	}

	// Should not panic.
	result := calc.CalculateBitRate(context.Background(), req)
	t.Logf("outputBR=0: bitrate=%d, isCritical=%v", int64(result.BitRate), result.IsCritical)

	// The result might be garbage due to Inf/NaN propagation. Verify it's
	// at least a valid number.
	require.False(t, math.IsNaN(float64(result.BitRate)), "BitRate should not be NaN")
	require.False(t, math.IsInf(float64(result.BitRate), 0), "BitRate should not be Inf")
}

// TestGapDecayGapDecayZero tests that gapDecay=0 does not panic.
// Go computes: -gapB.ToBps(US(d.GapDecay)) where ToBps divides by gapDecay.
func TestGapDecayGapDecayZero(t *testing.T) {
	calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: 3 * time.Second,
		QueueSizeMin:         200_000,
		GapDecay:             0, // division by zero trigger
		InertiaIncrease:      10 * time.Second,
		InertiaDecrease:      2 * time.Second,
		DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
	}

	cfg := &smtypes.AutoBitRateVideoConfig{
		CheckInterval: 1 * time.Second,
	}

	req := smtypes.CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		InputBitrate:          5_000_000,
		ActualOutputBitrate:   4_000_000,
		QueueDuration:         5 * time.Second,
		QueueSize:             0,
		QueueSizeDerivative:   100,
		Config:                cfg,
	}

	// Should not panic (Go float64 produces Inf, not panic).
	result := calc.CalculateBitRate(context.Background(), req)
	t.Logf("gapDecay=0: bitrate=%d, isCritical=%v", int64(result.BitRate), result.IsCritical)
}

// TestGapDecayLargeOverflow tests int64 overflow potential.
// outputBps * gapS could overflow int64 for large values in integer arithmetic.
// Go uses float64 throughout, so overflow manifests as precision loss instead.
func TestGapDecayLargeOverflow(t *testing.T) {
	calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: 1 * time.Second,
		QueueSizeMin:         0,
		GapDecay:             1 * time.Second,
		InertiaIncrease:      1 * time.Second,
		InertiaDecrease:      1 * time.Second,
		DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
	}

	cfg := &smtypes.AutoBitRateVideoConfig{
		CheckInterval: 1 * time.Second,
	}

	// outputBR = 50_000_000 bps, queueDur = 30s → outputBps * gap = 50M * 29
	// = 1.45 billion, fits in int64 but might lose float64 precision.
	req := smtypes.CalculateBitRateRequest{
		CurrentBitrateSetting: 50_000_000,
		InputBitrate:          50_000_000,
		ActualOutputBitrate:   50_000_000,
		QueueDuration:         30 * time.Second,
		QueueSize:             0,
		QueueSizeDerivative:   0,
		Config:                cfg,
	}

	result := calc.CalculateBitRate(context.Background(), req)
	t.Logf("large values: bitrate=%d, isCritical=%v", int64(result.BitRate), result.IsCritical)

	// When queueDur > optimal, bitrate should decrease.
	assert.Less(t, int64(result.BitRate), int64(50_000_000),
		"bitrate should decrease when queue is above optimal")
}

// TestGapDecayInertiaSignMismatch tests the case where bitRateDiff and
// (raw - current) have different signs. This happens when currentBR < 1
// and bitRateDiff < 0: raw = max(currentBR + bitRateDiff, 1) = 1,
// but 1 > currentBR, so raw - currentBR > 0 while bitRateDiff < 0.
func TestGapDecayInertiaSignMismatch(t *testing.T) {
	// With currentBR=0, any bitRateDiff produces raw = max(0+diff, 1).
	// If diff < 0, raw = 1, and raw - currentBR = 1 > 0.
	// Go branches on bitRateDiff < 0 (decrease path).
	// Old Lean spec branched on raw - current > 0 (increase path). Bug!
	calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: 3 * time.Second,
		QueueSizeMin:         0,
		GapDecay:             3 * time.Second,
		InertiaIncrease:      10 * time.Second,
		InertiaDecrease:      2 * time.Second,
		DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
	}

	cfg := &smtypes.AutoBitRateVideoConfig{
		CheckInterval: 1 * time.Second,
	}

	// Force bitRateDiff < 0 by having a large queue.
	req := smtypes.CalculateBitRateRequest{
		CurrentBitrateSetting: 0, // currentBR = 0 triggers the sign mismatch
		InputBitrate:          5_000_000,
		ActualOutputBitrate:   4_000_000,
		QueueDuration:         10 * time.Second,
		QueueSize:             0,
		QueueSizeDerivative:   0,
		Config:                cfg,
	}

	result := calc.CalculateBitRate(context.Background(), req)
	t.Logf("currentBR=0, large queue: bitrate=%d, isCritical=%v",
		int64(result.BitRate), result.IsCritical)

	// The result should be 1 (raw = max(0 + negative, 1) = 1).
	// The decrease branch inertia should clamp upward toward current (0),
	// but since raw=1 > current=0, the floor has no effect.
	assert.GreaterOrEqual(t, int64(result.BitRate), int64(1),
		"bitrate should be at least 1")
}

// TestGapDecayFloat64PrecisionLoss tests precision loss in the
// float64(v) / float64(time.Second) computation.
// For large UB values, float64 has only 53 bits of mantissa.
func TestGapDecayFloat64PrecisionLoss(t *testing.T) {
	// gapB close to 2^53 would lose precision in ToBps computation.
	// With reasonable inputs, gapB = outputBps * gap / 8.
	// If outputBps = 50M and gap = 30s, gapB ≈ 187.5M bytes — well within float64.
	// But let's verify no silent precision loss occurs.

	calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: 1 * time.Second,
		QueueSizeMin:         0,
		GapDecay:             3 * time.Second,
		InertiaIncrease:      10 * time.Second,
		InertiaDecrease:      2 * time.Second,
		DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
	}

	cfg := &smtypes.AutoBitRateVideoConfig{
		CheckInterval: 1 * time.Second,
	}

	req := smtypes.CalculateBitRateRequest{
		CurrentBitrateSetting: 100_000_000,
		InputBitrate:          100_000_000,
		ActualOutputBitrate:   99_999_999,
		QueueDuration:         30 * time.Second,
		QueueSize:             0,
		QueueSizeDerivative:   0,
		Config:                cfg,
	}

	result := calc.CalculateBitRate(context.Background(), req)
	t.Logf("large precision test: bitrate=%d", int64(result.BitRate))

	// Manually compute expected intermediate values with float64.
	gap := globaltypes.US(29 * time.Second) // 30 - 1 = 29 seconds
	gapBits := globaltypes.Ubps(99_999_999).Tob(gap)
	gapB := gapBits.ToB()
	t.Logf("gapBits=%d, gapB=%d", int64(gapBits), int64(gapB))

	// Verify the intermediates are exact at these magnitudes.
	expectedGapBits := int64(float64(99_999_999) * float64(29*time.Second) / float64(time.Second))
	assert.Equal(t, expectedGapBits, int64(gapBits), "gapBits precision check")
}

// TestGapDecayNegationSign tests the negation in desiredDerivative = -gapB.ToBps(decay).
// When gapB > 0 (queue above optimal), desiredDerivative should be negative (drain queue).
// When gapB < 0 (queue below optimal), desiredDerivative should be positive (fill queue).
func TestGapDecayNegationSign(t *testing.T) {
	tests := []struct {
		name     string
		queueDur time.Duration
		wantSign int // 1 for increase, -1 for decrease, 0 for equilibrium
	}{
		{"above_optimal", 10 * time.Second, -1},
		{"below_optimal", 1 * time.Second, 1},
		{"at_optimal", 3 * time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
				QueueDurationOptimal: 3 * time.Second,
				QueueSizeMin:         0,
				GapDecay:             3 * time.Second,
				InertiaIncrease:      10 * time.Second,
				InertiaDecrease:      2 * time.Second,
				DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
			}

			cfg := &smtypes.AutoBitRateVideoConfig{
				CheckInterval: 1 * time.Second,
			}

			req := smtypes.CalculateBitRateRequest{
				CurrentBitrateSetting: 5_000_000,
				InputBitrate:          5_000_000,
				ActualOutputBitrate:   4_000_000,
				QueueDuration:         tt.queueDur,
				QueueSize:             0,
				QueueSizeDerivative:   0,
				Config:                cfg,
			}

			result := calc.CalculateBitRate(context.Background(), req)
			br := int64(result.BitRate)

			switch tt.wantSign {
			case 1:
				assert.Greater(t, br, int64(5_000_000),
					"bitrate should increase when below optimal")
			case -1:
				assert.Less(t, br, int64(5_000_000),
					"bitrate should decrease when above optimal")
			case 0:
				assert.Equal(t, int64(5_000_000), br,
					"bitrate should stay the same at optimal")
			}
		})
	}
}
