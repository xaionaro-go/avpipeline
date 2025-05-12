package router

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	transcoder "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough"
	transcodertypes "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarderFromRouterRecoding[CS any, PS processor.Abstract] struct {
	Chain      *transcoder.TranscoderWithPassthrough[CS, PS]
	CancelFunc context.CancelFunc
}

var _ StreamForwarder[*Route, *ProcessorRouting] = (*StreamForwarderFromRouterRecoding[*Route, *ProcessorRouting])(nil)

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarderRecoding[CS any, PS processor.Abstract](
	ctx context.Context,
	src *node.NodeWithCustomData[CS, PS],
	dst node.Abstract,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *StreamForwarderFromRouterRecoding[CS, PS], _err error) {
	logger.Debugf(ctx, "NewStreamForwarderRecoding(%s, %s)", src, dst)
	defer func() { logger.Debugf(ctx, "/NewStreamForwarderRecoding(%s, %s): %p, %v", src, dst, _ret, _err) }()

	fwd := &StreamForwarderFromRouterRecoding[CS, PS]{}

	var err error
	fwd.Chain, err = transcoder.New(ctx, src, dst)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a StreamForward: %w", err)
	}

	if recoderConfig == nil {
		logger.Debugf(ctx, "just copy as is")
		recoderConfig = &transcodertypes.RecoderConfig{}
		if getPacketSourcer, ok := any(src.Processor).(interface{ GetPacketSource() packet.Source }); ok {
			if packetSource := getPacketSourcer.GetPacketSource(); packetSource != nil {
				packetSource.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
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
		}
	}
	logger.Debugf(ctx, "resulting config: %#+v", recoderConfig)
	if err := fwd.Chain.SetRecoderConfig(ctx, *recoderConfig); err != nil {
		return nil, fmt.Errorf("unable to set the RecoderConfig to %#+v: %w", *recoderConfig, err)
	}

	return fwd, nil
}

func (fwd *StreamForwarderFromRouterRecoding[CS, PS]) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()

	if fwd.CancelFunc != nil {
		return fmt.Errorf("internal error: already started")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn
	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()

	if err := fwd.Chain.Start(ctx, false); err != nil {
		return fmt.Errorf("unable to start the StreamForward: %w", err)
	}
	return nil
}

func (fwd *StreamForwarderFromRouterRecoding[CS, PS]) Source() *node.NodeWithCustomData[CS, PS] {
	return fwd.Chain.Input
}

func (fwd *StreamForwarderFromRouterRecoding[CS, PS]) Destination() node.Abstract {
	return fwd.Chain.Outputs[0]
}

func (fwd *StreamForwarderFromRouterRecoding[CS, PS]) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()

	if fwd.CancelFunc == nil {
		return ErrAlreadyClosed{}
	}
	fwd.CancelFunc()
	fwd.CancelFunc = nil
	fwd.Chain.Wait(ctx)
	return nil
}
