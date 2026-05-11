package android

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
)

func TestInitialCamera2NDKPTSReturnsExactSharedEpochDelta(t *testing.T) {
	const t0 = int64(20_000_000_000)
	const advanceNanos = int64(50_000_000)

	fake := newFakeMonotonicClock(t0)
	prev := kernel.SetMonotonicClock(fake)
	defer kernel.SetMonotonicClock(prev)
	kernel.ResetPTSEpochForTesting()
	defer kernel.ResetPTSEpochForTesting()

	require.Equal(t, t0, kernel.PTSEpochNanos())

	fake.SetNanos(t0 + advanceNanos)
	require.Equal(t, advanceNanos, initialCamera2NDKPTS())
}

func TestCamera2NDKPTSFromImageTimestampPreservesImageDelta(t *testing.T) {
	const firstPTS = int64(50_000_000)
	const firstTimestampNs = int64(1_000_000_000)
	const frameDurationNs = int64(33_333_333)

	require.Equal(t,
		firstPTS+10_000_000,
		camera2NDKPTSFromImageTimestamp(firstPTS, firstTimestampNs, firstTimestampNs+10_000_000, 1, frameDurationNs),
	)
	require.Equal(t,
		firstPTS+2*frameDurationNs,
		camera2NDKPTSFromImageTimestamp(firstPTS, firstTimestampNs, 0, 2, frameDurationNs),
	)
}
