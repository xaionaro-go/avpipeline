// shift_calibration_test.go tests the PTS/DTS shift calibration state machine.

package kernel

import (
	"testing"

	assertT "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShiftCalibration_FirstPacketIsMin covers the common case where
// the first observed packet holds the minimum DTS across all streams.
// Agent-generated test.
func TestShiftCalibration_FirstPacketIsMin(t *testing.T) {
	c := newShiftCalibration(true, true, 0, 0, 2, 60)

	shouldCommit := c.Observe(0, 1000, true, 1000, true)
	assertT.False(t, shouldCommit, "first of two streams should not trigger commit")

	shouldCommit = c.Observe(1, 1500, true, 1500, true)
	assertT.True(t, shouldCommit, "second of two streams should trigger commit")

	ptsShift, dtsShift, hasPTS, hasDTS := c.Commit()
	assertT.True(t, hasPTS)
	assertT.True(t, hasDTS)
	assertT.Equal(t, int64(-1000), ptsShift,
		"shift must land the minimum raw PTS at target 0")
	assertT.Equal(t, int64(-1000), dtsShift,
		"shift must land the minimum raw DTS at target 0")
}

// TestShiftCalibration_LaterPacketIsMin reproduces the bug: stream B's
// first packet has a lower DTS than stream A's first packet. The
// committed shift must land stream B's packet (the minimum) at the
// target, never stream A's.
// Agent-generated test.
func TestShiftCalibration_LaterPacketIsMin(t *testing.T) {
	c := newShiftCalibration(true, true, 0, 0, 2, 60)

	shouldCommit := c.Observe(0, 1000, true, 1000, true)
	assertT.False(t, shouldCommit)

	shouldCommit = c.Observe(1, 500, true, 500, true)
	assertT.True(t, shouldCommit)

	ptsShift, dtsShift, _, _ := c.Commit()
	assertT.Equal(t, int64(-500), ptsShift,
		"shift must track the minimum across streams (500), not the first packet (1000)")
	assertT.Equal(t, int64(-500), dtsShift,
		"shift must track the minimum across streams (500), not the first packet (1000)")

	// Applying the committed shift to both streams must leave every
	// shifted value at or above the target.
	assertT.GreaterOrEqual(t, int64(1000)+dtsShift, int64(0),
		"stream A's first packet DTS must be non-negative after shift")
	assertT.GreaterOrEqual(t, int64(500)+dtsShift, int64(0),
		"stream B's first packet DTS must be non-negative after shift")
}

// TestShiftCalibration_BufferFullForcesCommit verifies that the
// calibration commits once MaxBufferedPkts has been observed even if
// not every stream has contributed yet. Silent streams must not stall
// the pipeline indefinitely.
// Agent-generated test.
func TestShiftCalibration_BufferFullForcesCommit(t *testing.T) {
	// Three expected streams, but stream #2 never produces a packet.
	c := newShiftCalibration(true, true, 0, 0, 3, 5)

	for range 4 {
		shouldCommit := c.Observe(0, 1000, true, 1000, true)
		assertT.False(t, shouldCommit)
	}
	shouldCommit := c.Observe(1, 800, true, 800, true)
	assertT.True(t, shouldCommit, "buffer cap of 5 reached on the 5th observation")

	assertT.Equal(t, 2, c.StreamsSeen(),
		"diagnostic: only two of three streams produced packets before commit")
	_, dtsShift, _, _ := c.Commit()
	assertT.Equal(t, int64(-800), dtsShift,
		"shift must be computed from the minimum seen so far")
}

// TestShiftCalibration_NegativeTargetShiftDirection pins down the sign
// convention: the committed shift is (target - minRaw). A target above
// the minimum raw value yields a positive shift; a target below yields
// a negative shift.
// Agent-generated test.
func TestShiftCalibration_NegativeTargetShiftDirection(t *testing.T) {
	c := newShiftCalibration(true, true, 200, 200, 1, 60)
	c.Observe(0, 1000, true, 1000, true)
	_, dtsShift, _, hasDTS := c.Commit()
	require.True(t, hasDTS)
	assertT.Equal(t, int64(-800), dtsShift,
		"shift = target(200) - minRaw(1000) = -800")
}

// TestShiftCalibration_TargetAboveMin covers a target above the minimum
// raw value (the "start the output at this offset" case).
// Agent-generated test.
func TestShiftCalibration_TargetAboveMin(t *testing.T) {
	c := newShiftCalibration(true, true, 5000, 5000, 1, 60)
	c.Observe(0, 100, true, 100, true)
	ptsShift, dtsShift, _, _ := c.Commit()
	assertT.Equal(t, int64(4900), ptsShift, "shift = target(5000) - minRaw(100) = 4900")
	assertT.Equal(t, int64(4900), dtsShift, "shift = target(5000) - minRaw(100) = 4900")
}

// TestShiftCalibration_OnlyDTSWanted checks that the calibration does
// not track PTS statistics when wantPTS is false (an Input that set
// ForceStartPTS=PTSKeep but ForceStartDTS!=PTSKeep).
// Agent-generated test.
func TestShiftCalibration_OnlyDTSWanted(t *testing.T) {
	c := newShiftCalibration(false, true, 0, 0, 1, 60)
	c.Observe(0, 123, true, 456, true)
	ptsShift, dtsShift, hasPTS, hasDTS := c.Commit()
	assertT.False(t, hasPTS, "no PTS shift must be produced when wantPTS=false")
	assertT.True(t, hasDTS)
	assertT.Equal(t, int64(0), ptsShift)
	assertT.Equal(t, int64(-456), dtsShift)
}

// TestShiftCalibration_ActiveAfterCommit guards against re-entering
// calibration once it has committed.
// Agent-generated test.
func TestShiftCalibration_ActiveAfterCommit(t *testing.T) {
	c := newShiftCalibration(true, true, 0, 0, 1, 60)
	require.True(t, c.Active())
	c.Observe(0, 100, true, 100, true)
	c.Commit()
	assertT.False(t, c.Active(),
		"calibration must report inactive after committing")

	// Observing again should be a no-op.
	should := c.Observe(0, 50, true, 50, true)
	assertT.False(t, should)
}

// TestShiftCalibration_NoTimestampsNoShift verifies the calibration
// does not attempt to compute a shift when no packet ever provides a
// valid PTS/DTS.
// Agent-generated test.
func TestShiftCalibration_NoTimestampsNoShift(t *testing.T) {
	c := newShiftCalibration(true, true, 0, 0, 1, 60)
	c.Observe(0, 0, false, 0, false)
	_, _, hasPTS, hasDTS := c.Commit()
	assertT.False(t, hasPTS)
	assertT.False(t, hasDTS)
}
