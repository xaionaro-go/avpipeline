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
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarderRecoding[CS any, PS processor.Abstract] struct {
	Input               *node.NodeWithCustomData[CS, PS]
	InputAsPacketSource packet.Source
	DestinationNode     node.Abstract
	RecoderConfig       transcodertypes.RecoderConfig
	Chain               *transcoder.TranscoderWithPassthrough[CS, PS]
	ChainInput          *nodewrapper.NoServe[*node.Node[*processor.FromKernel[*kernel.MapStreamIndices]]]
	CancelFunc          context.CancelFunc
	Mutex               xsync.Mutex
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
		Input:               src,
		InputAsPacketSource: packetSource,
		DestinationNode:     dst,
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
	fwd.RecoderConfig = *recoderConfig

	return fwd, nil
}

func (fwd *StreamForwarderRecoding[CS, PS]) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Mutex, fwd.start, ctx)
}

func (fwd *StreamForwarderRecoding[CS, PS]) start(origCtx context.Context) (_err error) {
	logger.Debugf(origCtx, "start")
	defer func() { logger.Debugf(origCtx, "/start: %v", _err) }()
	if fwd.CancelFunc != nil {
		return fmt.Errorf("internal error: already started")
	}

	ctx, cancelFn := context.WithCancel(origCtx)
	fwd.CancelFunc = cancelFn
	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()
	chain, err := transcoder.New[CS, PS](ctx, fwd.InputAsPacketSource, &nodewrapper.NoServe[node.Abstract]{Node: fwd.DestinationNode})
	if err != nil {
		return fmt.Errorf("unable to initialize a StreamForward: %w", err)
	}
	fwd.Chain = chain
	type chainInputNode = node.Node[*processor.FromKernel[*kernel.MapStreamIndices]]
	if err := chain.SetRecoderConfig(ctx, fwd.RecoderConfig); err != nil {
		return fmt.Errorf("unable to set the RecoderConfig to %#+v: %w", fwd.RecoderConfig, err)
	}

	if err := chain.Start(ctx, false); err != nil {
		return fmt.Errorf("unable to start the StreamForward: %w", err)
	}

	fwd.ChainInput = &nodewrapper.NoServe[*chainInputNode]{Node: fwd.Chain.Input()}
	fwd.Input.AddPushPacketsTo(fwd.ChainInput)

	observability.Go(ctx, func() {
		logger.Debugf(ctx, "waiter")
		defer func() { logger.Debugf(ctx, "/waiter") }()
		err := chain.Wait(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to wait: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		logger.Errorf(ctx, "the recoder was unexpectedly closed, restarting it")
		err = xsync.DoR1(ctx, &fwd.Mutex, func() error {
			if fwd.CancelFunc == nil || fwd.Chain != chain {
				return nil // somebody else already closed between select above and DoR1 here
			}
			if err := fwd.stop(origCtx); err != nil {
				logger.Errorf(ctx, "unable to cleanup: %v", err)
			}
			return fwd.start(origCtx)
		})
		if err != nil {
			logger.Errorf(ctx, "unable to restart the recoder: %v", err)
		}
	})

	return nil
}

func (fwd *StreamForwarderRecoding[CS, PS]) Source() *node.NodeWithCustomData[CS, PS] {
	return fwd.Input
}

func (fwd *StreamForwarderRecoding[CS, PS]) Destination() node.Abstract {
	return fwd.DestinationNode
}

func (fwd *StreamForwarderRecoding[CS, PS]) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Mutex, fwd.stop, ctx)
}

func (fwd *StreamForwarderRecoding[CS, PS]) stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "stop")
	defer func() { logger.Debugf(ctx, "/stop: %v", _err) }()
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
