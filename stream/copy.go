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
	CopySideData(dst, src)
	CopyNonCodecParameters(dst, src)
	return nil
}

func CopySideData(
	dst, src *astiav.Stream,
) {
	// FFmpeg deprecated AVStream.side_data in favor of AVStream.codecpar.side_data.
	// See: https://ffmpeg.org/pipermail/ffmpeg-devel/2023-October/315398.html
	// The side data is now owned by AVCodecParameters (coded_side_data).
	// See: https://ffmpeg.org/doxygen/8.0/structAVCodecParameters.html#ad54da9241deabb3601e6e0e8fa832c19
	// Rotation metadata is stored in AV_PKT_DATA_DISPLAYMATRIX.
	// See: https://ffmpeg.org/doxygen/8.0/packet_8h.html#gga9a80bfcacc586b483a973272800edb97aab8c149a1e6c67aad340733becec87e1
	srcSideData := src.CodecParameters().SideData()
	if dm, ok := srcSideData.DisplayMatrix().Get(); ok {
		dstSideData := dst.CodecParameters().SideData()
		_ = dstSideData.DisplayMatrix().Add(dm)
	}
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
