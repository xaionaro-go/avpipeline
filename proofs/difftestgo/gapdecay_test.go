// gapdecay_test.go exercises the real QueueSizeGapDecay calculator and prints
// results in a format comparable to the Lean DiffTest harness.
//
// Test vectors are designed to exercise divergences between Go (float64) and
// Lean (Int) arithmetic.  (agent-generated test)
package difftestgo

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/xaionaro-go/avpipeline/indicator"
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// passthroughMA is a trivial MovingAverage that returns the exact input value
// and is always Valid.  This removes smoothing from the test so we can compare
// raw computation chains between Go and Lean.
type passthroughMA[T interface{ ~float64 | ~int64 }] struct {
	val T
	set bool
}

var _ indicator.MovingAverage[globaltypes.UBps] = (*passthroughMA[globaltypes.UBps])(nil)

func (m *passthroughMA[T]) Update(v T) T { m.val = v; m.set = true; return v }
func (m *passthroughMA[T]) InitPeriod() int64 { return 0 }
func (m *passthroughMA[T]) Valid() bool        { return m.set }

// gapdecayTestVector holds one test case (all times in integer seconds,
// rates in integer bits/s or bytes/s).
type gapdecayTestVector struct {
	name string

	// Config fields
	queueDurationOptimal int64 // seconds
	queueSizeMinBytes    int64 // bytes
	gapDecay             int64 // seconds
	inertiaIncrease      int64 // seconds
	inertiaDecrease      int64 // seconds

	// Request fields
	currentBR      int64 // bits/s (CurrentBitrateSetting)
	inputBR        int64 // bits/s (InputBitrate)
	outputBR       int64 // bits/s (ActualOutputBitrate)
	queueDur       int64 // seconds (QueueDuration)
	derivative     int64 // bytes/s (QueueSizeDerivative, pre-smoothing)
	checkInterval  int64 // seconds (Config.CheckInterval)
}

var gapdecayTestVectors = []gapdecayTestVector{
	{
		name:                 "typical",
		currentBR:            5_000_000,
		inputBR:              5_000_000,
		outputBR:             4_000_000,
		queueDur:             5,
		derivative:           100,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    200_000,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
	{
		name:                 "large_gapB_float64_precision",
		currentBR:            100_000_000,
		inputBR:              100_000_000,
		outputBR:             99_999_999,
		queueDur:             100,
		derivative:           1,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    200_000,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
	{
		name:                 "small_outputBR",
		currentBR:            1000,
		inputBR:              1000,
		outputBR:             1,
		queueDur:             10,
		derivative:           0,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    200_000,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
	{
		name:                 "negative_gap",
		currentBR:            5_000_000,
		inputBR:              5_000_000,
		outputBR:             4_000_000,
		queueDur:             1,
		derivative:           -500,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    200_000,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
	{
		name:                 "inertia_clamp_increase",
		currentBR:            1_000_000,
		inputBR:              10_000_000,
		outputBR:             10_000_000,
		queueDur:             50,
		derivative:           0,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    200_000,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
	{
		name:                 "equilibrium",
		currentBR:            5_000_000,
		inputBR:              5_000_000,
		outputBR:             4_000_000,
		queueDur:             3,
		derivative:           0,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    0,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
	{
		name:                 "currentBR_zero",
		currentBR:            0,
		inputBR:              5_000_000,
		outputBR:             4_000_000,
		queueDur:             5,
		derivative:           100,
		checkInterval:        1,
		queueDurationOptimal: 3,
		queueSizeMinBytes:    200_000,
		gapDecay:             3,
		inertiaIncrease:      10,
		inertiaDecrease:      2,
	},
}

func TestGapDecay(t *testing.T) {
	for _, tv := range gapdecayTestVectors {
		t.Run(tv.name, func(t *testing.T) {
			runGapDecayVector(t, tv)
		})
	}
}

// TestGapDecayProtocol runs vectors and prints lines suitable for automated
// diffing against the Lean DiffTest.
func TestGapDecayProtocol(t *testing.T) {
	for _, tv := range gapdecayTestVectors {
		runGapDecayVector(t, tv)
	}
}

func runGapDecayVector(t *testing.T, tv gapdecayTestVector) {
	t.Helper()

	// Print input line (for reference / Lean consumption).
	fmt.Printf("gapdecay %d %d %d %d %d %d %d %d %d %d %d\n",
		tv.currentBR, tv.inputBR, tv.outputBR, tv.queueDur, tv.derivative,
		tv.checkInterval, tv.queueDurationOptimal, tv.queueSizeMinBytes,
		tv.gapDecay, tv.inertiaIncrease, tv.inertiaDecrease)

	// Build calculator with passthrough MA.
	calc := &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
		QueueDurationOptimal: time.Duration(tv.queueDurationOptimal) * time.Second,
		QueueSizeMin:         globaltypes.UB(tv.queueSizeMinBytes),
		GapDecay:             time.Duration(tv.gapDecay) * time.Second,
		InertiaIncrease:      time.Duration(tv.inertiaIncrease) * time.Second,
		InertiaDecrease:      time.Duration(tv.inertiaDecrease) * time.Second,
		DerivativeSmoothed:   &passthroughMA[globaltypes.UBps]{},
	}

	cfg := &smtypes.AutoBitRateVideoConfig{
		CheckInterval: time.Duration(tv.checkInterval) * time.Second,
	}

	req := smtypes.CalculateBitRateRequest{
		CurrentBitrateSetting: globaltypes.Ubps(tv.currentBR),
		InputBitrate:          globaltypes.Ubps(tv.inputBR),
		ActualOutputBitrate:   globaltypes.Ubps(tv.outputBR),
		QueueDuration:         time.Duration(tv.queueDur) * time.Second,
		QueueSize:             0,
		QueueSizeDerivative:   globaltypes.UBps(tv.derivative),
		Config:                cfg,
	}

	// Call real calculator.
	result := calc.CalculateBitRate(context.Background(), req)

	// --- Manually trace intermediates (matching Go's unit chain) ---

	// queueDerivative = passthrough of req.QueueSizeDerivative
	queueDerivative := globaltypes.UBps(tv.derivative) // B/s, float64

	// queueDuration
	queueDuration := globaltypes.US(req.QueueDuration) // nanoseconds

	// queueDurationOptimal = max(configOptimal_ns, QueueSizeMin.Tob().ToS(outputBR))
	configOptimalNs := globaltypes.US(time.Duration(tv.queueDurationOptimal) * time.Second)
	minBytesToBits := globaltypes.UB(tv.queueSizeMinBytes).Tob() // Ub(int64)
	minDuration := minBytesToBits.ToS(globaltypes.Ubps(tv.outputBR))
	queueDurationOptimal := max(configOptimalNs, minDuration)

	// gap
	gap := queueDuration - queueDurationOptimal

	// gapB = outputBR.Tob(gap).ToB()
	gapBits := globaltypes.Ubps(tv.outputBR).Tob(gap)
	gapB := gapBits.ToB()

	// desiredDerivative = -gapB.ToBps(GapDecay_ns)
	gapDecayNs := globaltypes.US(time.Duration(tv.gapDecay) * time.Second)
	desiredDerivative := -gapB.ToBps(gapDecayNs)

	// derivativeGap = desiredDerivative - queueDerivative
	derivativeGap := desiredDerivative - queueDerivative

	// bitRateDiff = derivativeGap.Tobps()
	bitRateDiff := derivativeGap.Tobps()

	// rawNewBitRate = max(currentBR + bitRateDiff, 1)
	rawNewBR := max(globaltypes.Ubps(tv.currentBR)+bitRateDiff, 1)

	// isCritical
	isCritical := bitRateDiff < 0 && rawNewBR < max(globaltypes.Ubps(tv.outputBR), globaltypes.Ubps(tv.inputBR))/5

	// Print Go intermediates (float64 where applicable, nanoseconds for durations).
	fmt.Printf("go_intermediates %.17g %.17g %d %.17g %.17g %.17g %.17g %.17g\n",
		float64(queueDurationOptimal)/float64(time.Second), // seconds (float)
		float64(gap)/float64(time.Second),                   // seconds (float)
		int64(gapB),                                         // bytes (int)
		float64(desiredDerivative),                          // B/s
		float64(derivativeGap),                              // B/s
		float64(bitRateDiff),                                // b/s
		float64(rawNewBR),                                   // b/s
		float64(result.BitRate),                             // b/s (after inertia)
	)

	// Print Go intermediates as truncated integers (for comparison with Lean).
	queueDurOptInt := int64(math.Floor(float64(queueDurationOptimal) / float64(time.Second)))
	gapInt := int64(tv.queueDur) - queueDurOptInt
	gapBInt := int64(gapB)
	desiredDerivInt := int64(desiredDerivative)
	derivGapInt := int64(derivativeGap)
	bitRateDiffInt := int64(bitRateDiff)
	rawNewBRInt := int64(rawNewBR)
	finalBRInt := int64(result.BitRate)

	fmt.Printf("go_as_int %d %d %d %d %d %d %d %d\n",
		queueDurOptInt, gapInt, gapBInt,
		desiredDerivInt, derivGapInt, bitRateDiffInt,
		rawNewBRInt, finalBRInt)

	isCriticalStr := "false"
	if result.IsCritical {
		isCriticalStr = "true"
	}
	fmt.Printf("go_result %d %s\n", int64(result.BitRate), isCriticalStr)

	// Also compute isCritical on raw (pre-inertia) for diagnostics.
	isCriticalRawStr := "false"
	if isCritical {
		isCriticalRawStr = "true"
	}
	fmt.Printf("go_result_raw %d %s\n", int64(rawNewBR), isCriticalRawStr)
	fmt.Println("---")
}
