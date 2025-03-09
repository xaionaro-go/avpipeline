package avpipeline

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type StreamConfigurerCopy struct {
	GetOutputStreamer GetOutputStreamer
}

var _ StreamConfigurer = (*StreamConfigurerCopy)(nil)

type GetOutputStreamer interface {
	GetOutputStream(ctx context.Context, streamIndex int) *astiav.Stream
}

func NewStreamConfigurerCopy(
	getOutputStreamer GetOutputStreamer,
) *StreamConfigurerCopy {
	return &StreamConfigurerCopy{
		GetOutputStreamer: getOutputStreamer,
	}
}

func (sc *StreamConfigurerCopy) StreamConfigure(
	ctx context.Context,
	stream *astiav.Stream,
	pkt *astiav.Packet,
) error {
	return CopyStreamParameters(
		ctx,
		stream,
		sc.GetOutputStreamer.GetOutputStream(ctx, pkt.StreamIndex()),
	)
}

func CopyStreamParameters(
	ctx context.Context,
	dst, src *astiav.Stream,
) error {
	if err := src.CodecParameters().Copy(dst.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy the codec parameters of stream: %w", err)
	}
	copyNonCodecStreamParameters(dst, src)
	return nil
}

func copyNonCodecStreamParameters(
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
