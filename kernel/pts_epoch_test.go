// pts_epoch_test.go covers the shared PTS epoch helper used by media kernels
// (camera, microphone, ...) to anchor first-frame PTS to a common monotonic
// origin so audio and video do not desync at multi-input start. Tests use a
// deterministic fake MonotonicClock so assertions are exact (no real-time
// tolerance windows).

package kernel

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/asticode/go-astiav"
	assertT "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// fakeMonotonicClock is a deterministic MonotonicClock used by every test in
// this file. It returns whatever nanoseconds the test stored via SetNanos,
// so deltas are exact and there is zero coupling to the real system clock.
// Concurrent reads/writes are race-safe via atomic.Int64.
type fakeMonotonicClock struct {
	nanos atomic.Int64
}

func newFakeMonotonicClock(initial int64) *fakeMonotonicClock {
	c := &fakeMonotonicClock{}
	c.nanos.Store(initial)
	return c
}

func (c *fakeMonotonicClock) NanosSinceBoot() int64 {
	return c.nanos.Load()
}

func (c *fakeMonotonicClock) SetNanos(v int64) {
	c.nanos.Store(v)
}

func (c *fakeMonotonicClock) AdvanceNanos(d int64) {
	c.nanos.Add(d)
}

// installFakeClock swaps in a fake clock and resets the shared epoch. The
// caller defers the returned restore func so the production clock is back
// in place at test exit.
func installFakeClock(t *testing.T, initialNanos int64) *fakeMonotonicClock {
	t.Helper()
	fake := newFakeMonotonicClock(initialNanos)
	prev := SetMonotonicClock(fake)
	resetPTSEpochForTest()
	t.Cleanup(func() {
		SetMonotonicClock(prev)
		resetPTSEpochForTest()
	})
	return fake
}

// resetPTSEpochForTest is the package-internal alias for
// ResetPTSEpochForTesting used throughout the kernel-package tests so
// the call sites stay short. The exported reset is the canonical seam;
// this wrapper exists purely to keep existing in-package call sites
// readable without forcing a long type-prefixed name.
func resetPTSEpochForTest() {
	ResetPTSEpochForTesting()
}

func TestPTSEpoch_FirstCallInitializesToFakeClockReading(t *testing.T) {
	const initial = int64(7_777_000_000_000) // 7777s since fake boot
	installFakeClock(t, initial)
	got := PTSEpochNanos()
	require.Equal(t, initial, got,
		"first PTSEpochNanos call must capture the fake clock reading exactly")
}

func TestPTSEpoch_ZeroClockReadingPromotedToOne(t *testing.T) {
	// A monotonic reading of exactly 0 collides with the
	// "uninitialised" atomic.Int64 sentinel; the helper must promote
	// it to 1ns so the stored epoch is always non-zero.
	installFakeClock(t, 0)
	got := PTSEpochNanos()
	require.Equal(t, int64(1), got,
		"zero monotonic reading must be promoted to 1ns to dodge the sentinel")
}

func TestPTSEpoch_SubsequentCallsReturnSameValue(t *testing.T) {
	fake := installFakeClock(t, 1_000_000_000)
	first := PTSEpochNanos()
	require.Equal(t, int64(1_000_000_000), first)
	for i := 0; i < 5; i++ {
		fake.AdvanceNanos(1_000_000) // 1ms
		require.Equal(t, first, PTSEpochNanos(),
			"every subsequent call must return the original epoch even as the clock advances")
	}
}

func TestPTSEpoch_ConcurrentInitReturnsSingleEpoch(t *testing.T) {
	installFakeClock(t, 5_000_000_000)
	const goroutines = 64
	results := make([]int64, goroutines)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx] = PTSEpochNanos()
		}(i)
	}
	close(start)
	wg.Wait()

	first := results[0]
	for i, got := range results {
		require.Equal(t, first, got,
			"concurrent caller %d observed a different epoch", i)
	}
	require.Equal(t, int64(5_000_000_000), first,
		"concurrent epoch must equal the fake clock reading at first call")
}

func TestPTSSinceEpochInTimeBase_ZeroAtEpoch(t *testing.T) {
	// At monotonic time == epoch, the delta in any timebase must be 0.
	const epoch = int64(1_000_000_000_000)
	tb := astiav.NewRational(1, 90000)
	got := ptsSinceEpochInTimeBase(epoch, epoch, tb)
	assertT.Equal(t, int64(0), got)
}

func TestPTSSinceEpochInTimeBase_AudioSampleRate(t *testing.T) {
	// 1.5 seconds after epoch, in 1/48000 timebase, must be 72_000 samples.
	const epoch = int64(1_000_000_000_000)
	const oneAndHalfSecondNanos = int64(1_500_000_000)
	now := epoch + oneAndHalfSecondNanos
	tb := astiav.NewRational(1, 48000)
	got := ptsSinceEpochInTimeBase(now, epoch, tb)
	assertT.Equal(t, int64(72_000), got)
}

func TestPTSSinceEpochInTimeBase_VideoMicroseconds(t *testing.T) {
	// 250ms after epoch in 1/1_000_000 (microseconds) → 250_000 ticks.
	const epoch = int64(1_000_000_000_000)
	const quarterSecondNanos = int64(250_000_000)
	now := epoch + quarterSecondNanos
	tb := astiav.NewRational(1, 1_000_000)
	got := ptsSinceEpochInTimeBase(now, epoch, tb)
	assertT.Equal(t, int64(250_000), got)
}

func TestPTSSinceEpochInTimeBase_ZeroOrInvalidTimeBaseReturnsZero(t *testing.T) {
	// A zero numerator (uninitialised rational) must not panic and must
	// not produce a bogus value — return 0 so callers fall through to
	// their existing pts=0 behaviour.
	const epoch = int64(1_000_000_000_000)
	const oneSecondNanos = int64(1_000_000_000)
	now := epoch + oneSecondNanos
	got := ptsSinceEpochInTimeBase(now, epoch, astiav.NewRational(0, 0))
	assertT.Equal(t, int64(0), got)
}

func TestResolvePTSShiftTarget_PassesThroughLiteralValue(t *testing.T) {
	// A literal target (e.g. 0 from the legacy ForceStartPTS=0 path)
	// must be returned unchanged regardless of timebase.
	got := resolvePTSShiftTarget(0, astiav.NewRational(1, 90000))
	assertT.Equal(t, int64(0), got)

	got = resolvePTSShiftTarget(12345, astiav.NewRational(1, 1_000_000))
	assertT.Equal(t, int64(12345), got)
}

func TestResolvePTSShiftTarget_EpochSentinelReturnsExactDeltaInTimeBase(t *testing.T) {
	// Install the fake at t0, force PTSEpochNanos to seed at t0, then
	// advance the clock by exactly 100ms. The helper's resolve path
	// reads the active monotonic clock against the cached epoch, so
	// the delta in the 1/48000 timebase is exactly 4_800 samples.
	const t0 = int64(2_000_000_000)
	const advanceNanos = int64(100_000_000) // 100ms
	fake := installFakeClock(t, t0)
	require.Equal(t, t0, PTSEpochNanos(), "epoch must seed at t0 before clock advances")
	fake.AdvanceNanos(advanceNanos)
	got := resolvePTSShiftTarget(globaltypes.PTSEpoch, astiav.NewRational(1, 48000))
	assertT.Equal(t, int64(4_800), got)
}

func TestPTSSinceEpochInTimeBase_PublicHelper(t *testing.T) {
	// Install fake at t0, seed epoch at t0, then advance by exactly
	// 250ms; public helper must return exactly 250_000 ticks in
	// 1/1_000_000 timebase.
	const t0 = int64(3_000_000_000)
	const advanceNanos = int64(250_000_000)
	fake := installFakeClock(t, t0)
	require.Equal(t, t0, PTSEpochNanos(), "epoch must seed at t0 before clock advances")
	fake.AdvanceNanos(advanceNanos)
	tb := astiav.NewRational(1, 1_000_000)
	got := PTSSinceEpochInTimeBase(tb)
	assertT.Equal(t, int64(250_000), got)
}

// TestPTSEpoch_TwoKernelsAlignedFirstFramesShareEpoch simulates the two
// kernels (camera + microphone) starting their first emitted frame at
// different monotonic-clock readings. After rebasing each first-frame
// PTS through the shared epoch, the two PTS values converted back to
// monotonic nanos via their own timebases must reproduce the actual
// cold-start delta (~890ms in this scenario) — exact, not "within 5ms".
func TestPTSEpoch_TwoKernelsAlignedFirstFramesShareEpoch(t *testing.T) {
	const t0 = int64(10_000_000_000) // 10s since fake boot
	fake := installFakeClock(t, t0)

	// Force epoch initialisation to t0.
	require.Equal(t, t0, PTSEpochNanos())

	// Microphone first frame at t0+10ms (AAudio fast start).
	micTB := astiav.NewRational(1, 48000)
	const micFirstOffsetNanos = int64(10_000_000)
	fake.SetNanos(t0 + micFirstOffsetNanos)
	micPTS := PTSSinceEpochInTimeBase(micTB)
	assertT.Equal(t, int64(480), micPTS, "10ms in 1/48000 timebase = 480 samples")

	// Camera first frame at t0+900ms (Camera2 cold start).
	camTB := astiav.NewRational(1, 1_000_000)
	const camFirstOffsetNanos = int64(900_000_000)
	fake.SetNanos(t0 + camFirstOffsetNanos)
	camPTS := PTSSinceEpochInTimeBase(camTB)
	assertT.Equal(t, int64(900_000), camPTS, "900ms in 1/1_000_000 timebase = 900_000 ticks")

	// Convert both back to monotonic nanos and compare deltas.
	micWallNanos := micPTS * int64(1_000_000_000) / int64(micTB.Den())
	camWallNanos := camPTS * int64(1_000_000_000) / int64(camTB.Den())
	deltaNanos := camWallNanos - micWallNanos
	assertT.Equal(t, int64(890_000_000), deltaNanos,
		"camera-vs-mic first-frame delta must be exactly 890ms")
}

// TestSetMonotonicClock_RestoresPriorClock proves the testing seam
// returns the previously-installed clock so callers can restore it.
// Identity is checked via behaviour (the fake returns a unique value)
// rather than pointer equality because the active clock lives behind
// an interface and Go does not guarantee stable boxed-pointer identity.
func TestSetMonotonicClock_RestoresPriorClock(t *testing.T) {
	original := CurrentMonotonicClock()
	require.NotNil(t, original, "production init must install a real clock")

	const fakeReading = int64(424242)
	fake := newFakeMonotonicClock(fakeReading)
	prev := SetMonotonicClock(fake)
	require.NotNil(t, prev, "SetMonotonicClock must return the previously-installed clock")
	assertT.Equal(t, fakeReading, CurrentMonotonicClock().NanosSinceBoot(),
		"fake must be active after install")

	restored := SetMonotonicClock(prev)
	assertT.Equal(t, fakeReading, restored.NanosSinceBoot(),
		"restoring must return the fake we just installed")
	current := CurrentMonotonicClock()
	require.NotNil(t, current)
	assertT.NotEqual(t, fakeReading, current.NanosSinceBoot(),
		"original clock must be reinstalled (real clock would not return our fake's sentinel)")
}

// TestRealMonotonicClock_IsMonotonic proves the production clock impl
// is non-decreasing across rapid successive calls. This is the only
// real-clock test in the file; everything else uses the fake.
func TestRealMonotonicClock_IsMonotonic(t *testing.T) {
	c := newRealMonotonicClock()
	prev := c.NanosSinceBoot()
	for i := 0; i < 1000; i++ {
		now := c.NanosSinceBoot()
		require.GreaterOrEqual(t, now, prev,
			"realMonotonicClock must be non-decreasing (iteration %d: prev=%d now=%d)", i, prev, now)
		prev = now
	}
}
