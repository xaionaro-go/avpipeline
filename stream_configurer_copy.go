package avpipeline

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type StreamConfigurerCopy struct {
	GetStreamer GetStreamer
}

var _ StreamConfigurer = (*StreamConfigurerCopy)(nil)

type GetStreamer interface {
	GetStream(ctx context.Context, streamIndex int) *astiav.Stream
}

func NewStreamConfigurerCopy(
	getStreamer GetStreamer,
) *StreamConfigurerCopy {
	return &StreamConfigurerCopy{
		GetStreamer: getStreamer,
	}
}

func (sc *StreamConfigurerCopy) StreamConfigure(
	ctx context.Context,
	stream *astiav.Stream,
	pkt *astiav.Packet,
) error {
	sampleStream := sc.GetStreamer.GetStream(ctx, pkt.StreamIndex())

	if err := sampleStream.CodecParameters().Copy(stream.CodecParameters()); err != nil {
		return fmt.Errorf("unable to copy the codec parameters of stream #%d: %w", pkt.StreamIndex(), err)
	}
	stream.SetDiscard(sampleStream.Discard())
	stream.SetAvgFrameRate(sampleStream.AvgFrameRate())
	stream.SetRFrameRate(sampleStream.RFrameRate())
	stream.SetSampleAspectRatio(sampleStream.SampleAspectRatio())
	stream.SetTimeBase(sampleStream.TimeBase())
	stream.SetStartTime(sampleStream.StartTime())
	stream.SetEventFlags(sampleStream.EventFlags())
	stream.SetPTSWrapBits(sampleStream.PTSWrapBits())
	return nil
}
