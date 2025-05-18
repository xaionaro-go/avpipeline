package autoheaders

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/node"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

func tryNewBSFForInBandHeaders[T any](
	ctx context.Context,
	videoCodecID astiav.CodecID,
	audioCodecID astiav.CodecID,
) (_ret *Node[T]) {
	logger.Debugf(ctx, "tryNewBSFForInBandHeaders(ctx, '%s', '%s')", videoCodecID, audioCodecID)
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForInBandHeaders(ctx, '%s', '%s'): %s", videoCodecID, audioCodecID, spew.Sdump(_ret))
	}()
	recoderVideoBSFName := bitstreamfilter.NameMP4ToMP2(videoCodecID)
	if recoderVideoBSFName == bitstreamfilter.NameNull {
		return nil
	}

	m := map[packetcondition.Condition]bitstreamfilter.Name{
		packetcondition.MediaType(astiav.MediaTypeVideo): recoderVideoBSFName,
	}
	logger.Debugf(ctx, "tryNewBSFForInBandHeaders(ctx, '%s', '%s'): m: %s", spew.Sdump(m))
	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, m)
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter '%s': %w", recoderVideoBSFName, err)
		return nil
	}

	return node.NewWithCustomDataFromKernel[T](
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}

func tryNewBSFForOOBHeaders[T any](
	ctx context.Context,
	videoCodecID astiav.CodecID,
	audioCodecID astiav.CodecID,
) (_ret *Node[T]) {
	logger.Debugf(ctx, "tryNewBSFForOOBHeaders(ctx, '%s', '%s')", videoCodecID, audioCodecID)
	defer func() {
		logger.Debugf(ctx, "/tryNewBSFForOOBHeaders(ctx, '%s', '%s'): %s", videoCodecID, audioCodecID, spew.Sdump(_ret))
	}()
	recoderVideoBSFName := bitstreamfilter.NameMP2ToMP4(videoCodecID)
	recoderAudioBSFName := bitstreamfilter.NameMP2ToMP4(audioCodecID)
	if recoderVideoBSFName == bitstreamfilter.NameNull && recoderAudioBSFName == bitstreamfilter.NameNull {
		return nil
	}

	m := map[packetcondition.Condition]bitstreamfilter.Name{
		packetcondition.MediaType(astiav.MediaTypeVideo): recoderVideoBSFName,
		packetcondition.MediaType(astiav.MediaTypeAudio): recoderAudioBSFName,
	}
	logger.Debugf(ctx, "tryNewBSFForMPEG4(ctx, '%s', '%s'): m: %s", videoCodecID, audioCodecID, spew.Sdump(m))
	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, m)
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filters '%s'/'%s': %w", recoderVideoBSFName, recoderAudioBSFName, err)
		return nil
	}

	return node.NewWithCustomDataFromKernel[T](
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}

func getCodecNamesFromStreams(streams []*astiav.Stream) (astiav.CodecID, astiav.CodecID) {
	var videoCodecID, audioCodecID astiav.CodecID
	for _, stream := range streams {
		switch stream.CodecParameters().MediaType() {
		case astiav.MediaTypeVideo:
			videoCodecID = stream.CodecParameters().CodecID()
		case astiav.MediaTypeAudio:
			audioCodecID = stream.CodecParameters().CodecID()
		}
	}
	return videoCodecID, audioCodecID
}
