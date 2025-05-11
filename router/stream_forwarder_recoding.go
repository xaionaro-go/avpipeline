package router

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	transcoder "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough"
	transcodertypes "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/node"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarderRecoding struct {
	Chain      *transcoder.TranscoderWithPassthrough[*Route, *ProcessorRouting]
	CancelFunc context.CancelFunc
}

var _ StreamForwarder = (*StreamForwarderRecoding)(nil)

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarderRecoding(
	ctx context.Context,
	src *NodeRouting,
	dst node.Abstract,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *StreamForwarderRecoding, _err error) {
	logger.Debugf(ctx, "NewStreamForwarderRecoding(%s, %s)", src, dst)
	defer func() { logger.Debugf(ctx, "/NewStreamForwarderRecoding(%s, %s): %p, %v", src, dst, _ret, _err) }()

	fwd := &StreamForwarderRecoding{}

	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn
	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()

	var err error
	fwd.Chain, err = transcoder.New(ctx, src, dst)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a StreamForward: %w", err)
	}

	if recoderConfig == nil {
		// just copy as is
		recoderConfig = &transcodertypes.RecoderConfig{}
		src.Processor.Kernel.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			for _, stream := range fmtCtx.Streams() {
				switch stream.CodecParameters().MediaType() {
				case astiav.MediaTypeVideo:
					recoderConfig.VideoTracks = append(recoderConfig.VideoTracks, transcodertypes.TrackConfig{
						InputTrackIDs: []int{stream.Index()},
						CodecName:     "copy",
					})
				case astiav.MediaTypeAudio:
					recoderConfig.AudioTracks = append(recoderConfig.AudioTracks, transcodertypes.TrackConfig{
						InputTrackIDs: []int{stream.Index()},
						CodecName:     "copy",
					})
				}
			}
		})
	}
	logger.Debugf(ctx, "resulting config: %#+v", recoderConfig)
	if err := fwd.Chain.SetRecoderConfig(ctx, *recoderConfig); err != nil {
		return nil, fmt.Errorf("unable to set the RecoderConfig to %#+v: %w", *recoderConfig, err)
	}

	return fwd, nil
}

func (fwd *StreamForwarderRecoding) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	if err := fwd.Chain.Start(ctx, false); err != nil {
		return fmt.Errorf("unable to start the StreamForward: %w", err)
	}
	return nil
}

func (fwd *StreamForwarderRecoding) Source() *NodeRouting {
	return fwd.Chain.Input
}

func (fwd *StreamForwarderRecoding) Destination() node.Abstract {
	return fwd.Chain.Outputs[0]
}

func (fwd *StreamForwarderRecoding) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	fwd.CancelFunc()
	fwd.Chain.Wait(ctx)
	return nil
}
