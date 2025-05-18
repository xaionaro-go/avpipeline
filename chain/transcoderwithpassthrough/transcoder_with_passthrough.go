package transcoderwithpassthrough

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/chain/autoheaders"
	"github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	nodecondition "github.com/xaionaro-go/avpipeline/node/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packet/filter"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/quality"
	avptypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	rescaleTS                  = false
	notifyAboutPacketSources   = true
	startWithPassthrough       = false
	autoInsertBitstreamFilters = true
	passthroughSupport         = true
)

type TranscoderWithPassthrough[C any, P processor.Abstract] struct {
	Input                  *node.NodeWithCustomData[C, P]
	Outputs                []node.Abstract
	FilterThrottle         *packetcondition.VideoAverageBitrateLower
	PassthroughSwitch      *packetcondition.Switch
	PostSwitchFilter       *packetcondition.Switch
	BothPipesSwitch        *packetcondition.Static
	Recoder                *kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	MapInputStreamIndices  *kernel.MapStreamIndices
	MapOutputStreamIndices *kernel.MapStreamIndices
	NodeRecoder            *node.Node[*processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]

	RecodingConfig types.RecoderConfig

	inputAsPacketSource                 packet.Source
	inputStreamMapIndicesAsPacketSource packet.Source

	locker    xsync.Mutex
	waitGroup sync.WaitGroup
}

/*
//                           +--> THROTTLE ->---+
// INPUT --> MAP INDICES ->--+                  +--> MAP INDICES --> OUTPUT
//                           +--> RECODER -->---+
*/
func New[C any, P processor.Abstract](
	ctx context.Context,
	input *node.NodeWithCustomData[C, P],
	outputs ...node.Abstract,
) (*TranscoderWithPassthrough[C, P], error) {
	s := &TranscoderWithPassthrough[C, P]{
		Input:             input,
		Outputs:           outputs,
		FilterThrottle:    packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		PassthroughSwitch: packetcondition.NewSwitch(),
		PostSwitchFilter:  packetcondition.NewSwitch(),
		BothPipesSwitch:   ptr(packetcondition.Static(false)),
	}
	swCond := packetcondition.And{
		packetcondition.MediaType(astiav.MediaTypeVideo),
		packetcondition.IsKeyFrame(true),
	}
	s.PassthroughSwitch.SetKeepUnless(swCond)
	s.PassthroughSwitch.SetOnAfterSwitch(func(ctx context.Context, from, to int32) {
		logger.Debugf(ctx, "s.PostSwitchFilter.SetValue(ctx, %d)", to)
		err := s.PostSwitchFilter.SetValue(ctx, to)
		logger.Debugf(ctx, "/s.PostSwitchFilter.SetValue(ctx, %d): %v", to, err)
	})
	s.PostSwitchFilter.SetKeepUnless(swCond)
	s.MapInputStreamIndices = kernel.NewMapStreamIndices(ctx, newStreamIndexAssignerInput(ctx, s))
	s.MapOutputStreamIndices = kernel.NewMapStreamIndices(ctx, newStreamIndexAssignerOutput(s))
	s.inputAsPacketSource = asPacketSource(s.Input.GetProcessor())
	if s.inputAsPacketSource == nil {
		return nil, fmt.Errorf("the input node processor is expected to be a packet source, but is not: %T", s.Input.GetProcessor())
	}

	return s, nil
}

func (s *TranscoderWithPassthrough[C, P]) GetRecoderConfig(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	logger.Tracef(ctx, "GetRecoderConfig")
	defer func() { logger.Tracef(ctx, "/GetRecoderConfig: %v", _ret) }()
	return xsync.DoA1R1(ctx, &s.locker, s.getRecoderConfigLocked, ctx)
}

func (s *TranscoderWithPassthrough[C, P]) getRecoderConfigLocked(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	switchValue := s.PassthroughSwitch.GetValue(ctx)
	logger.Tracef(ctx, "switchValue: %v", switchValue)
	if switchValue == 0 {
		return s.RecodingConfig
	}
	cpy := s.RecodingConfig
	cpy.VideoTracks = slices.Clone(cpy.VideoTracks)
	cpy.VideoTracks[0].CodecName = codec.CodecNameCopy
	return cpy
}

func (s *TranscoderWithPassthrough[C, P]) SetRecoderConfig(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "SetRecoderConfig(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/SetRecoderConfig(ctx, %#+v): %v", cfg, _err) }()
	return xsync.DoA2R1(ctx, &s.locker, s.setRecoderConfigLocked, ctx, cfg)
}

func (s *TranscoderWithPassthrough[C, P]) setRecoderConfigLocked(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	err := s.configureRecoder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to configure the recoder: %w", err)
	}
	s.RecodingConfig = cfg
	s.MapInputStreamIndices.Assigner.(*streamIndexAssignerInput[C, P]).reload(ctx)
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) configureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) error {
	if len(cfg.VideoTracks) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track (received a request for %d tracks)", len(cfg.VideoTracks))
	}
	if len(cfg.AudioTracks) != 1 {
		return fmt.Errorf("currently we support only exactly one output audio track (received a request for %d tracks)", len(cfg.AudioTracks))
	}
	if s.Recoder == nil {
		if err := s.initRecoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to initialize the recoder: %w", err)
		}
		return nil
	}
	if cfg.AudioTracks[0].CodecName != "copy" {
		return fmt.Errorf("we currently do not support audio recoding: '%s' != 'copy'", cfg.AudioTracks[0].CodecName)
	}
	if cfg.VideoTracks[0].CodecName == "copy" {
		if err := s.reconfigureRecoderCopy(ctx, cfg); err != nil {
			return fmt.Errorf("unable to reconfigure to copying: %w", err)
		}
		return nil
	}
	if err := s.reconfigureRecoder(ctx, cfg); err != nil {
		return fmt.Errorf("unable to reconfigure the recoder: %w", err)
	}
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) initRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) error {
	if s.Recoder != nil {
		return fmt.Errorf("internal error: an encoder is already initialized")
	}

	var err error
	s.Recoder, err = kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx,
			avptypes.HardwareDeviceType(cfg.VideoTracks[0].HardwareDeviceType),
			avptypes.HardwareDeviceName(cfg.VideoTracks[0].HardwareDeviceName),
			nil,
			nil,
		),
		codec.NewNaiveEncoderFactory(ctx,
			cfg.VideoTracks[0].CodecName,
			"copy",
			avptypes.HardwareDeviceType(cfg.VideoTracks[0].HardwareDeviceType),
			avptypes.HardwareDeviceName(cfg.VideoTracks[0].HardwareDeviceName),
			convertCustomOptions(cfg.VideoTracks[0].CustomOptions),
			convertCustomOptions(cfg.AudioTracks[0].CustomOptions),
		),
		nil,
	)
	if err != nil {
		return fmt.Errorf("unable to initialize a recoder: %w", err)
	}
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) reconfigureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) error {
	encoderFactory := s.Recoder.EncoderFactory
	if cfg.VideoTracks[0].CodecName != encoderFactory.VideoCodec {
		return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", cfg.VideoTracks[0].CodecName, encoderFactory.VideoCodec)
	}

	err := xsync.DoR1(ctx, &s.Recoder.EncoderFactory.Locker, func() error {
		if len(s.Recoder.EncoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			if s.Recoder.EncoderFactory.VideoOptions == nil {
				s.Recoder.EncoderFactory.VideoOptions = astiav.NewDictionary()
				setFinalizerFree(ctx, s.Recoder.EncoderFactory.VideoOptions)
			}

			if cfg.VideoTracks[0].AverageBitRate == 0 {
				s.Recoder.EncoderFactory.VideoOptions.Unset("b")
			} else {
				s.Recoder.EncoderFactory.VideoOptions.Set("b", fmt.Sprintf("%d", cfg.VideoTracks[0].AverageBitRate), 0)
			}
			return nil
		}

		logger.Debugf(ctx, "the encoder is already initialized, so modifying it if needed")
		encoder := s.Recoder.EncoderFactory.VideoEncoders[0]

		q := encoder.GetQuality(ctx)
		if q == nil {
			logger.Errorf(ctx, "unable to get the current encoding quality")
			q = quality.ConstantBitrate(0)
		}

		needsChangingBitrate := true
		if q, ok := q.(quality.ConstantBitrate); ok {
			if q == quality.ConstantBitrate(cfg.VideoTracks[0].AverageBitRate) {
				needsChangingBitrate = false
			}
		}

		if needsChangingBitrate && cfg.VideoTracks[0].AverageBitRate > 0 {
			err := encoder.SetQuality(ctx, quality.ConstantBitrate(cfg.VideoTracks[0].AverageBitRate), nil)
			if err != nil {
				return fmt.Errorf("unable to set bitrate to %v: %w", cfg.VideoTracks[0].AverageBitRate, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = s.PassthroughSwitch.SetValue(ctx, 0)
	if err != nil {
		return fmt.Errorf("unable to switch the pre-filter to recoding: %w", err)
	}

	return nil
}

func (s *TranscoderWithPassthrough[C, P]) reconfigureRecoderCopy(
	ctx context.Context,
	cfg types.RecoderConfig,
) error {
	err := s.PassthroughSwitch.SetValue(ctx, 1)
	if err != nil {
		return fmt.Errorf("unable to switch the pre-filter to passthrough: %w", err)
	}
	s.FilterThrottle.BitrateAveragingPeriod = cfg.VideoTracks[0].AveragingPeriod
	s.FilterThrottle.AverageBitRate = cfg.VideoTracks[0].AverageBitRate // if AverageBitRate != 0 then here we also enable the throttler (if it was disabled)
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) GetAllStats(
	ctx context.Context,
) map[string]*node.ProcessingStatistics {
	m := map[string]*node.ProcessingStatistics{
		"Recoder": s.NodeRecoder.GetStats(),
	}
	tryGetStats := func(key string, n node.Abstract) {
		getter, ok := n.(interface {
			GetStats() *node.ProcessingStatistics
		})
		if !ok {
			return
		}
		m[key] = getter.GetStats()
	}
	tryGetStats("Input", s.Input)
	for idx, output := range s.Outputs {
		tryGetStats(fmt.Sprintf("Output%d", idx), output)
	}
	return m
}

func asPacketSource(proc processor.Abstract) packet.Source {
	if getPacketSourcer, ok := proc.(interface{ GetPacketSource() packet.Source }); ok {
		if packetSource := getPacketSourcer.GetPacketSource(); packetSource != nil {
			return packetSource
		}
	}
	return nil
}

func asPacketSink(proc processor.Abstract) packet.Sink {
	if getPacketSinker, ok := proc.(interface{ GetPacketSink() packet.Sink }); ok {
		if packetSink := getPacketSinker.GetPacketSink(); packetSink != nil {
			return packetSink
		}
	}
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) Start(
	ctx context.Context,
	recoderInSeparateTracks bool,
) (_err error) {
	logger.Debugf(ctx, "Start(ctx, %t)", recoderInSeparateTracks)
	defer logger.Debugf(ctx, "/Start(ctx, %t): %v", recoderInSeparateTracks, _err)
	if s.Recoder == nil {
		return fmt.Errorf("s.Recoder is not configured")
	}
	if len(s.Outputs) != 1 {
		return fmt.Errorf("currently we support only the case with a single output, but received %d outputs", len(s.Outputs))
	}
	output := s.Outputs[0]
	outputAsPacketSink := asPacketSink(output.GetProcessor())
	if outputAsPacketSink == nil {
		return fmt.Errorf("the output node processor is expected to be a packet sink, but is not")
	}

	// == configure ==

	ctx, cancelFnOrig := context.WithCancel(ctx)
	var cancelOnce sync.Once
	cancelFn := func() {
		cancelOnce.Do(func() {
			logger.Debugf(ctx, "Serve: cancel")
		})
		cancelFnOrig()
	}

	s.NodeRecoder = node.NewFromKernel(
		ctx,
		s.Recoder,
		processor.DefaultOptionsRecoder()...,
	)
	nodeFilterThrottle := node.NewFromKernel(
		ctx,
		kernel.NewPacketFilter(s.FilterThrottle, nil),
		processor.DefaultOptionsOutput()...,
	)

	var recoderOutput node.Abstract = s.NodeRecoder
	var nodeBSFPassthrough *node.Node[*processor.FromKernel[*kernel.BitstreamFilter]]
	{
		nodeBSFRecoder := autoheaders.NewNode(
			ctx,
			s.Recoder.Encoder,
			outputAsPacketSink,
		)
		nodeBSFPassthrough = autoheaders.NewNode(
			ctx,
			s.inputAsPacketSource,
			outputAsPacketSink,
		)

		if autoInsertBitstreamFilters && nodeBSFRecoder != nil {
			logger.Debugf(ctx, "inserting %s to the recoder's output", nodeBSFRecoder.Processor.Kernel)
			recoderOutput.AddPushPacketsTo(nodeBSFRecoder)
			recoderOutput = nodeBSFRecoder
		}
	}

	mapInputStreamIndicesNode := node.NewFromKernel(
		ctx,
		s.MapInputStreamIndices,
		processor.DefaultOptionsRecoder()...,
	)
	s.inputStreamMapIndicesAsPacketSource = asPacketSource(mapInputStreamIndicesNode.Processor)
	s.Input.AddPushPacketsTo(mapInputStreamIndicesNode)

	if passthroughSupport {
		audioFrameCount := 0
		keyFrameCount := 0
		bothPipesSwitch := packetcondition.And{
			packetcondition.Static(recoderInSeparateTracks),
			s.BothPipesSwitch,
			packetcondition.Or{
				packetcondition.And{
					packetcondition.IsKeyFrame(true),
					packetcondition.MediaType(astiav.MediaTypeVideo),
					packetcondition.Function(func(ctx context.Context, pkt packet.Input) bool {
						keyFrameCount++
						if keyFrameCount <= 1 {
							logger.Debugf(ctx, "frame size: %d", len(pkt.Data()))
							return true
						}
						return false
					}),
				},
				packetcondition.And{
					packetcondition.MediaType(astiav.MediaTypeAudio),
					packetcondition.Function(func(ctx context.Context, pkt packet.Input) bool {
						audioFrameCount++
						if audioFrameCount <= 1 {
							logger.Debugf(ctx, "frame size: %d", len(pkt.Data()))
							return true
						}
						return false
					}),
				},
				packetcondition.Not{
					packetcondition.MediaType(astiav.MediaTypeAudio),
					packetcondition.MediaType(astiav.MediaTypeVideo),
				},
			},
		}

		var passthroughOutput node.Abstract = nodeFilterThrottle
		if autoInsertBitstreamFilters && nodeBSFPassthrough != nil {
			logger.Debugf(ctx, "inserting %s to the passthrough output", nodeBSFPassthrough.Processor.Kernel)
			passthroughOutput.AddPushPacketsTo(nodeBSFPassthrough)
			passthroughOutput = nodeBSFPassthrough
		}

		mapInputStreamIndicesNode.AddPushPacketsTo(
			s.NodeRecoder,
			packetcondition.Or{
				packetcondition.And{
					s.PassthroughSwitch.Condition(0),
					s.PostSwitchFilter.Condition(0),
				},
				bothPipesSwitch,
			},
		)
		mapInputStreamIndicesNode.AddPushPacketsTo(
			nodeFilterThrottle,
			packetcondition.Or{
				packetcondition.And{
					s.PassthroughSwitch.Condition(1),
					s.PostSwitchFilter.Condition(1),
				},
				bothPipesSwitch,
			},
		)

		if startWithPassthrough {
			s.PassthroughSwitch.CurrentValue.Store(1)
			s.PostSwitchFilter.CurrentValue.Store(1)
			s.PassthroughSwitch.NextValue.Store(1)
			s.PostSwitchFilter.NextValue.Store(1)
		}

		if recoderInSeparateTracks {
			*s.BothPipesSwitch = true
			nodeMapStreamIndices := node.NewFromKernel(
				ctx,
				s.MapOutputStreamIndices,
				processor.DefaultOptionsOutput()...,
			)
			recoderOutput.AddPushPacketsTo(
				nodeMapStreamIndices,
			)
			passthroughOutput.AddPushPacketsTo(
				nodeMapStreamIndices,
			)
			nodeMapStreamIndices.AddPushPacketsTo(output)
		} else {
			if rescaleTS && (!startWithPassthrough || notifyAboutPacketSources) {
				nodeFilterThrottle.InputPacketCondition = packetcondition.And{
					filter.NewRescaleTSBetweenKernels(
						s.inputAsPacketSource,
						s.NodeRecoder.Processor.Kernel.Encoder,
					),
				}
			} else {
				logger.Warnf(ctx, "unable to configure rescale_ts because startWithPassthrough && !notifyAboutPacketSources")
			}

			recoderOutput.AddPushPacketsTo(
				output,
				packetcondition.And{
					s.PassthroughSwitch.Condition(0),
					s.PostSwitchFilter.Condition(0),
				},
			)
			passthroughOutput.AddPushPacketsTo(
				output,
				packetcondition.And{
					s.PassthroughSwitch.Condition(1),
					s.PostSwitchFilter.Condition(1),
				},
			)
		}
	} else {
		mapInputStreamIndicesNode.AddPushPacketsTo(s.NodeRecoder)
		recoderOutput.AddPushPacketsTo(output)
	}

	removeSubscriptionToInput := func(ctx context.Context) error {
		var errs []error
		if err := node.RemovePushPacketsTo(ctx, s.Input, mapInputStreamIndicesNode); err != nil {
			errs = append(errs, fmt.Errorf("unable to remove packet pushing from Input to %s: %w", mapInputStreamIndicesNode, err))
		}
		return errors.Join(errs...)
	}

	defer func() {
		if _err != nil {
			err := removeSubscriptionToInput(ctx)
			if err != nil {
				logger.Error(ctx, "unable to cleanup packet pushing: %v", err)
			}
		}
	}()

	// == spawn an observer ==

	errCh := make(chan node.Error, 100)
	s.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer s.waitGroup.Done()
		defer cancelFn()
		logger.Debugf(ctx, "Serve: started the error listening loop")
		defer logger.Debugf(ctx, "Serve: finished the error listening loop")
		for {
			select {
			case err := <-ctx.Done():
				logger.Debugf(ctx, "stopping listening for errors: %v", err)
				return
			case err, ok := <-errCh:
				if !ok {
					logger.Debugf(ctx, "the error channel is closed")
					return
				}
				if errors.Is(err.Err, node.ErrAlreadyStarted{}) {
					logger.Errorf(ctx, "%#+v", err)
					continue
				}
				cancelFn()
				if errors.Is(err.Err, context.Canceled) {
					logger.Debugf(ctx, "cancelled: %#+v", err)
					continue
				}
				if errors.Is(err.Err, io.EOF) {
					logger.Debugf(ctx, "EOF: %#+v", err)
					continue
				}
				logger.Errorf(ctx, "stopping because received error: %v", err)
				return
			}
		}
	})

	// == prepare ==

	if notifyAboutPacketSources {
		err := avpipeline.NotifyAboutPacketSources(ctx, nil, s.Input)
		if err != nil {
			return fmt.Errorf("receive an error while notifying nodes about packet sources: %w", err)
		}
	}
	logger.Infof(ctx, "resulting pipeline: %s", s.Input.String())
	logger.Infof(ctx, "resulting pipeline: %s", s.Input.DotString(false))

	// == launch ==

	s.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer s.waitGroup.Done()
		defer cancelFn()
		defer logger.Debugf(ctx, "finished the serving routine")
		defer func() {
			if err := removeSubscriptionToInput(ctx); err != nil {
				logger.Error(ctx, "unable to cleanup packet pushing: %v", err)
			}
		}()

		avpipeline.Serve(ctx, avpipeline.ServeConfig{
			NodeFilter:     nodecondition.Not{nodecondition.In{s.Input}},
			NodeTreeFilter: nodecondition.Not{nodecondition.In(s.Outputs)},
		}, errCh, s.Input)
	})

	return nil
}

func (s *TranscoderWithPassthrough[C, P]) Wait(
	ctx context.Context,
) error {
	s.waitGroup.Wait()
	return nil
}
