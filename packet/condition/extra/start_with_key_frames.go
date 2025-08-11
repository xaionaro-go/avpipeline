package extra

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type StartWithKeyFrames struct {
	WaitingKeyFrames map[int]bool
	Started          bool
	Locker           xsync.Mutex
}

func NewStartWithKeyFrames() *StartWithKeyFrames {
	return &StartWithKeyFrames{
		WaitingKeyFrames: make(map[int]bool),
	}
}

func (c *StartWithKeyFrames) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return xsync.DoA2R1(ctx, &c.Locker, c.match, ctx, pkt)
}

func (c *StartWithKeyFrames) match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	if c.Started {
		return true
	}

	pkt.Source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		if fmtCtx.NbStreams() != len(c.WaitingKeyFrames) {
			c.acknowledgeNewStreams(fmtCtx)
		}
	})

	streamIndex := pkt.GetStreamIndex()
	waitingKeyFrame, ok := c.WaitingKeyFrames[streamIndex]
	if !ok {
		logger.Errorf(ctx, "internal error: stream %d is not initialized in this condition", streamIndex)
	}

	if !waitingKeyFrame {
		return true
	}

	isKeyFrame := pkt.Packet.Flags().Has(astiav.PacketFlagKey)
	if !isKeyFrame {
		return false
	}

	c.WaitingKeyFrames[streamIndex] = false
	return true
}

func (c *StartWithKeyFrames) String() string {
	ctx := context.TODO()
	if !c.Locker.ManualTryRLock(ctx) {
		return fmt.Sprintf("StartWithKeyFrames")
	}
	defer c.Locker.ManualRUnlock(ctx)
	b, _ := json.Marshal(c.WaitingKeyFrames)
	return fmt.Sprintf("StartWithKeyFrames(%s)", b)
}

func (c *StartWithKeyFrames) acknowledgeNewStreams(fmtCtx *astiav.FormatContext) {
	for _, stream := range fmtCtx.Streams() {
		streamIndex := stream.Index()
		if _, ok := c.WaitingKeyFrames[streamIndex]; ok {
			continue
		}
		c.WaitingKeyFrames[streamIndex] = stream.CodecParameters().MediaType() == astiav.MediaTypeVideo
	}
}
