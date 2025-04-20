package main

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

func tryNewBSF(
	ctx context.Context,
	codecID astiav.CodecID,
) *avpipeline.Node[*processor.FromKernel[*kernel.BitstreamFilter]] {
	recoderBSFName := bitstreamfilter.NameMP4ToAnnexB(codecID)
	if recoderBSFName == bitstreamfilter.NameNull {
		return nil
	}

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, map[condition.Condition]bitstreamfilter.Name{
		condition.MediaType(astiav.MediaTypeVideo): recoderBSFName,
	})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter '%s': %w", recoderBSFName, err)
		return nil
	}

	return avpipeline.NewNodeFromKernel(
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}

func getVideoCodecNameFromStreams(streams []*astiav.Stream) astiav.CodecID {
	for _, stream := range streams {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			return stream.CodecParameters().CodecID()
		}
	}
	return astiav.CodecIDNone
}
