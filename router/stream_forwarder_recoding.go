package router

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/packet"
	transcoder "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/processor"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarderRecoding[CS any, PS processor.Abstract] struct {
	Input      *node.NodeWithCustomData[CS, PS]
	Chain      *transcoder.TranscoderWithPassthrough[CS, PS]
	ChainInput *nodewrapper.NoServe[*node.Node[*processor.FromKernel[*kernel.MapStreamIndices]]]
	CancelFunc context.CancelFunc
}

var _ StreamForwarder[*Route, *ProcessorRouting] = (*StreamForwarderRecoding[*Route, *ProcessorRouting])(nil)

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarderRecoding[CS any, PS processor.Abstract](
	ctx context.Context,
	src *node.NodeWithCustomData[CS, PS],
	dst node.Abstract,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret *StreamForwarderRecoding[CS, PS], _err error) {
	logger.Debugf(ctx, "NewStreamForwarderRecoding(%s, %s)", src, dst)
	defer func() { logger.Debugf(ctx, "/NewStreamForwarderRecoding(%s, %s): %p, %v", src, dst, _ret, _err) }()

	packetSource := asPacketSource(src.Processor)
	if packetSource == nil {
		return nil, fmt.Errorf("the source is expected to provide packet.Source")
	}

	fwd := &StreamForwarderRecoding[CS, PS]{
		Input: src,
	}

	var err error
	fwd.Chain, err = transcoder.New[CS, PS](ctx, packetSource, &nodewrapper.NoServe[node.Abstract]{Node: dst})
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a StreamForward: %w", err)
	}
	type chainInputNode = node.Node[*processor.FromKernel[*kernel.MapStreamIndices]]
	fwd.ChainInput = &nodewrapper.NoServe[*chainInputNode]{Node: fwd.Chain.Input()}

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
		if len(recoderConfig.AudioTracks) == 0 && len(recoderConfig.VideoTracks) == 0 {
			logger.Errorf(ctx, "no audio/video tracks defined, adding one of each just to make it work")
			recoderConfig.VideoTracks = append(recoderConfig.VideoTracks, transcodertypes.TrackConfig{
				InputTrackIDs: []int{0, 1, 2, 3, 4, 5, 6, 7},
				CodecName:     "copy",
			})
			recoderConfig.AudioTracks = append(recoderConfig.AudioTracks, transcodertypes.TrackConfig{
				InputTrackIDs: []int{0, 1, 2, 3, 4, 5, 6, 7},
				CodecName:     "copy",
			})
		}
	}
	logger.Debugf(ctx, "resulting config: %#+v", recoderConfig)
	if err := fwd.Chain.SetRecoderConfig(ctx, *recoderConfig); err != nil {
		return nil, fmt.Errorf("unable to set the RecoderConfig to %#+v: %w", *recoderConfig, err)
	}

	return fwd, nil
}

func (fwd *StreamForwarderRecoding[CS, PS]) Start(ctx context.Context) (_err error) {
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
	fwd.Input.AddPushPacketsTo(fwd.ChainInput)

	return nil
}

func (fwd *StreamForwarderRecoding[CS, PS]) Source() *node.NodeWithCustomData[CS, PS] {
	return fwd.Input
}

func (fwd *StreamForwarderRecoding[CS, PS]) Destination() node.Abstract {
	return fwd.Chain.Outputs[0]
}

func (fwd *StreamForwarderRecoding[CS, PS]) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()

	if fwd.CancelFunc == nil {
		return ErrAlreadyClosed{}
	}
	fwd.CancelFunc()
	fwd.CancelFunc = nil
	removePushErr := node.RemovePushPacketsTo(ctx, fwd.Input, fwd.ChainInput)
	if removePushErr != nil {
		return fmt.Errorf("unable to remove pushing packets from %s to %s", fwd.Input, fwd.ChainInput.Node)
	}
	fwd.Chain.Wait(ctx)
	return nil
}
