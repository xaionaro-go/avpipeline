package filter

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

type RescaleTSBetweenKernels struct {
	From packet.Source
	To   packet.Sink

	Locker     xsync.Mutex
	FromStream map[int]*astiav.Stream
	ToStream   map[int]*astiav.Stream
}

var _ condition.Condition = (*RescaleTSBetweenKernels)(nil)

func NewRescaleTSBetweenKernels(
	from packet.Source,
	to packet.Sink,
) *RescaleTSBetweenKernels {
	return &RescaleTSBetweenKernels{
		From:       from,
		To:         to,
		FromStream: make(map[int]*astiav.Stream),
		ToStream:   make(map[int]*astiav.Stream),
	}
}

func (f *RescaleTSBetweenKernels) getFromStream(
	ctx context.Context,
	streamIndex int,
) *astiav.Stream {
	return xsync.DoR1(ctx, &f.Locker, func() *astiav.Stream {
		s, ok := f.FromStream[streamIndex]
		if ok {
			return s
		}
		f.From.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			for _, stream := range fmtCtx.Streams() {
				if stream.Index() == streamIndex {
					s = stream
					break
				}
			}
		})
		f.FromStream[streamIndex] = s
		return s
	})
}

func (f *RescaleTSBetweenKernels) getToStream(
	ctx context.Context,
	streamIndex int,
) *astiav.Stream {
	return xsync.DoR1(ctx, &f.Locker, func() *astiav.Stream {
		s, ok := f.ToStream[streamIndex]
		if ok {
			return s
		}
		f.To.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			for _, stream := range fmtCtx.Streams() {
				if stream.Index() == streamIndex {
					s = stream
					break
				}
			}
		})
		f.ToStream[streamIndex] = s
		return s
	})
}

func (f *RescaleTSBetweenKernels) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	streamFrom := f.getFromStream(ctx, pkt.StreamIndex())
	streamTo := f.getToStream(ctx, pkt.StreamIndex())
	pkt.RescaleTs(streamFrom.TimeBase(), streamTo.TimeBase())
	return true
}

func (f *RescaleTSBetweenKernels) String() string {
	return fmt.Sprintf("RescaleTSBetweenKernels(%s -> %s)", f.From, f.To)
}
