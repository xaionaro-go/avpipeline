package main

import (
	"context"

	"github.com/asticode/go-astiav"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
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

	return sw
}
