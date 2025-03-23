package avconv

import (
	"time"

	"github.com/asticode/go-astiav"
)

const (
	// see https://ffmpeg.org/doxygen/trunk/group__lavu__time.html#ga2eaefe702f95f619ea6f2d08afa01be1
	avNoPTSValue = uint64(0x8000000000000000)
)

func Duration(t int64, timeBase astiav.Rational) time.Duration {
	if uint64(t) == avNoPTSValue {
		return -1
	}

	timeBaseF64 := float64(timeBase.Num()) / float64(timeBase.Den())
	seconds := float64(t) * timeBaseF64
	return time.Duration(seconds * float64(time.Second))
}
