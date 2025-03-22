package kernel

import (
	"context"

	"github.com/asticode/go-astiav"

	"github.com/xaionaro-go/avpipeline/stream"
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
	s *astiav.Stream,
	pkt *astiav.Packet,
) error {
	return stream.CopyParameters(
		ctx,
		s,
		sc.GetOutputStreamer.GetOutputStream(ctx, pkt.StreamIndex()),
	)
}
