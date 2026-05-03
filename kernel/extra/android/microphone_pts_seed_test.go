// microphone_pts_seed_test.go covers the shared-epoch seed used by
// the AAudio microphone capture loop. Lives without build tags so the
// helper is exercised on the dev host even when there is no Android
// runtime available. Uses a deterministic fake MonotonicClock injected
// via kernel.SetMonotonicClock so assertions are exact, with no
// real-time tolerance windows.

package android

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
)

// fakeMonotonicClock mirrors the kernel-package fake but lives here
// because Go test helpers cannot cross package boundaries. The
// MonotonicClock interface is exported, so any consumer can supply a
// trivial implementation like this one without depending on the
// kernel package's internals.
type fakeMonotonicClock struct {
	nanos atomic.Int64
}

func newFakeMonotonicClock(initial int64) *fakeMonotonicClock {
	c := &fakeMonotonicClock{}
	c.nanos.Store(initial)
	return c
}

func (c *fakeMonotonicClock) NanosSinceBoot() int64 { return c.nanos.Load() }
func (c *fakeMonotonicClock) SetNanos(v int64)      { c.nanos.Store(v) }

func TestInitialMicrophonePTS_NonPositiveSampleRateReturnsZero(t *testing.T) {
	assert.Equal(t, int64(0), initialMicrophonePTS(0))
	assert.Equal(t, int64(0), initialMicrophonePTS(-1))
}

func TestInitialMicrophonePTS_PositiveSampleRateReturnsExactDelta(t *testing.T) {
	// Inject a deterministic clock: epoch seeded at t0, then advance
	// by exactly 50ms. The seed for sampleRate=48000 must equal
	// 50ms * 48000 / 1s = 2_400 samples — exact, no tolerance.
	const t0 = int64(20_000_000_000) // 20s since fake boot
	const advanceNanos = int64(50_000_000)
	const sampleRate = 48000

	fake := newFakeMonotonicClock(t0)
	prev := kernel.SetMonotonicClock(fake)
	defer kernel.SetMonotonicClock(prev)
	kernel.ResetPTSEpochForTesting()
	defer kernel.ResetPTSEpochForTesting()

	// Seed the shared epoch deterministically at t0.
	require.Equal(t, t0, kernel.PTSEpochNanos())

	fake.SetNanos(t0 + advanceNanos)
	got := initialMicrophonePTS(sampleRate)
	assert.Equal(t, int64(2_400), got,
		"50ms in 1/48000 timebase = 2_400 samples (exact)")
}
