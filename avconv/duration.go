// duration.go provides utilities for converting between FFmpeg timestamps and time.Duration.

// Package avconv provides conversion utilities for media types and values.
package avconv

import (
	"math"
	"time"

	"github.com/asticode/go-astiav"
)

const (
	// see https://ffmpeg.org/doxygen/trunk/group__lavu__time.html#ga2eaefe702f95f619ea6f2d08afa01be1
	avNoPTSValue = uint64(0x8000000000000000)
)

const (
	noDuration = time.Duration(math.MinInt64)
)

func init() {
	if avNoPTSValue != uint64(any(int64(math.MinInt64)).(int64)) { // to bypass the compiler check
		panic("avNoPTSValue changed")
	}
}

func Duration(t int64, timeBase astiav.Rational) time.Duration {
	if uint64(t) == avNoPTSValue {
		return noDuration
	}

	return time.Duration(float64(t) * timeBase.Float64() * float64(time.Second))
}

func FromDuration(d time.Duration, timeBase astiav.Rational) int64 {
	if d == noDuration {
		return math.MinInt64 // equivalent to avNoPTSValue
	}

	return int64(d.Seconds() / timeBase.Float64())
}
