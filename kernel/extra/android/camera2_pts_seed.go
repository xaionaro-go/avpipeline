package android

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
)

func initialCamera2NDKPTS() int64 {
	tb := astiav.NewRational(1, camera2NDKTimeBaseDen)
	return kernel.PTSSinceEpochInTimeBase(tb)
}

func camera2NDKPTSFromImageTimestamp(
	firstPTS int64,
	firstTimestampNs int64,
	timestampNs int64,
	frameIndex int64,
	frameDurationNs int64,
) int64 {
	if timestampNs > 0 && firstTimestampNs >= 0 {
		return firstPTS + timestampNs - firstTimestampNs
	}
	return firstPTS + frameIndex*frameDurationNs
}
