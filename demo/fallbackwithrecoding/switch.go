package main

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

func newSwitch(
	ctx context.Context,
) *barrierstategetter.Switch {
	sw := barrierstategetter.NewSwitch()
	sw.CurrentValue.Store(0)

	keepUnlessConds := packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.IsKeyFrame(true),
	}
	logger.Debugf(ctx, "setting keep-unless conditions: %s", keepUnlessConds)
	sw.SetKeepUnless(keepUnlessConds)

	sw.SetOnBeforeSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "SetOnBeforeSwitch: %d -> %d", from, to)
	})

	sw.SetOnAfterSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		if v := in.Get(); v != nil {
			ctx = belt.WithField(ctx, "media_type", v.GetMediaType().String())
		}
		logger.Debugf(ctx, "SetOnAfterSwitch: %d -> %d", from, to)
	})

	return sw
}
