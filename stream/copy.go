// Package stream provides utilities for managing and manipulating AV streams.
//
// copy.go provides functions to copy parameters between streams.
package stream

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

func CopyParameters(
	ctx context.Context,
	dst, src *astiav.Stream,
) error {
	if err := src.CodecParameters().Copy(dst.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy the codec parameters of stream: %w", err)
	}
	CopyNonCodecParameters(dst, src)
	return nil
}

func CopyNonCodecParameters(
	dst, src *astiav.Stream,
) {
	dst.SetDiscard(src.Discard())
	dst.SetAvgFrameRate(src.AvgFrameRate())
	dst.SetRFrameRate(src.RFrameRate())
	dst.SetSampleAspectRatio(src.SampleAspectRatio())
	dst.SetTimeBase(src.TimeBase())
	dst.SetStartTime(src.StartTime())
	dst.SetEventFlags(src.EventFlags())
	dst.SetPTSWrapBits(src.PTSWrapBits())
}
