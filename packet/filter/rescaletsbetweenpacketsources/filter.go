package rescaletsbetweenpacketsources

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

type Filter struct {
	From packet.Source
	To   packet.Source

	Locker     xsync.Mutex
	FromStream map[int]*astiav.Stream
	ToStream   map[int]*astiav.Stream
}

var _ condition.Condition = (*Filter)(nil)

func New(
	from packet.Source,
	to packet.Source,
) *Filter {
	return &Filter{
		From:       from,
		To:         to,
		FromStream: make(map[int]*astiav.Stream),
		ToStream:   make(map[int]*astiav.Stream),
	}
}

func (f *Filter) getFromStream(
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

func (f *Filter) getToStream(
	ctx context.Context,
	streamIndex int,
) *astiav.Stream {
	return xsync.DoR1(ctx, &f.Locker, func() *astiav.Stream {
		s, ok := f.ToStream[streamIndex]
		if ok {
			return s
		}
		f.To.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
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

func (f *Filter) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	streamFrom := f.getFromStream(ctx, pkt.GetStreamIndex())
	streamTo := f.getToStream(ctx, pkt.GetStreamIndex())
	pkt.RescaleTs(streamFrom.TimeBase(), streamTo.TimeBase())
	return true
}

func (f *Filter) String() string {
	return fmt.Sprintf("RescaleTSBetweenKernels(%s -> %s)", f.From, f.To)
}
