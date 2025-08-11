package main

import (
	"context"
	"fmt"
	"time"

	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/quality"
)

func sillyAlternationsJustForDemonstration(
	sw *kernel.Switch[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]],
	alternateBitrate []int,
) func(ctx context.Context, pkt packet.Input) bool {
	recoderIdx := uint(0)
	bitrateIdx := 0
	pastSwitchPts := time.Duration(0)
	return func(ctx context.Context, pkt packet.Input) bool {
		pts := avconv.Duration(pkt.Pts(), pkt.Stream.TimeBase())
		logger.Tracef(ctx, "pts: %v (%v, %v)", pts, pkt.Pts(), pkt.Stream.TimeBase())
		if pts-pastSwitchPts < time.Second {
			return true
		}
		pastSwitchPts = pts
		if bitrateIdx >= len(alternateBitrate) {
			if len(alternateBitrate) > 0 {
				bitrateIdx = bitrateIdx % len(alternateBitrate)
			}

			// switch recoder

			recoderIdx++
			if recoderIdx >= uint(len(sw.Kernels)) {
				recoderIdx = 0
			}
			if sw != nil {
				err := sw.SetKernelIndex(ctx, recoderIdx)
				assert(ctx, err == nil)
			}
			logger.Debugf(ctx, "switch encoder to #%d", recoderIdx)
		}

		if len(alternateBitrate) == 0 {
			logger.Debugf(ctx, "len(alternateBitrate) == 0")
			return true
		}

		// switch bitrate

		nextBitrate := alternateBitrate[bitrateIdx]
		bitrateIdx++
		encoderFactory := sw.Kernels[recoderIdx].EncoderFactory
		encoderFactory.Locker.Do(ctx, func() {
			for _, encoder := range encoderFactory.VideoEncoders {
				err := encoder.SetQuality(
					ctx,
					quality.ConstantBitrate(nextBitrate),
					nil,
				)
				if err != nil {
					logger.Errorf(ctx, "SetQuality errored: %v", err)
					continue
				}
			}
		})
		fmt.Printf("changed the video bitrate to %d\n", nextBitrate)
		return true
	}
}
