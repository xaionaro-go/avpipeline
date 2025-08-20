package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packet/filter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/quality"
	avptypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	passthroughRescaleTS     = false
	notifyAboutPacketSources = true
	startWithPassthrough     = false
)

type InputNode[C any] = node.NodeWithCustomData[
	C, *processor.FromKernel[*boilerplate.BaseWithFormatContext[*InputHandler]],
]

type StreamMux[C any] struct {
	InputNode      *InputNode[C]
	MuxMode        types.MuxMode
	Outputs        []*Output[C]
	OutputsMap     map[OutputKey]*Output[C]
	OutputFactory  OutputFactory
	InputSyncer    *barrierstategetter.Switch
	OutputSyncer   *barrierstategetter.Switch
	RecodingConfig types.RecoderConfig
	Locker         xsync.Mutex

	waitGroup sync.WaitGroup
}

type OutputFactory interface {
	NewOutput(
		ctx context.Context,
		outputKey OutputKey,
	) (node.Abstract, error)
}

func New(
	ctx context.Context,
	muxMode types.MuxMode,
	outputFactory OutputFactory,
) (*StreamMux[struct{}], error) {
	return NewWithCustomData[struct{}](
		ctx,
		muxMode,
		outputFactory,
	)
}

func NewWithCustomData[C any](
	ctx context.Context,
	muxMode types.MuxMode,
	outputFactory OutputFactory,
) (*StreamMux[C], error) {
	s := &StreamMux[C]{
		InputSyncer:   barrierstategetter.NewSwitch(),
		OutputSyncer:  barrierstategetter.NewSwitch(),
		OutputFactory: outputFactory,
		OutputsMap:    map[OutputKey]*Output[C]{},
	}

	s.InputSyncer.SetKeepUnless(packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.IsKeyFrame(true),
	})
	var (
		prevDataLocker             sync.Mutex
		prevSource                 packet.Source
		prevOutputID               int32
		outputSyncerSwitchDeadline time.Time
	)

	type switchNotifierPacket struct {
		SwitchFromOutputID int32
	}

	// We want to notify the OutputSyncer that it needs to switch to the next output, and
	// send a packet that would notify that there are no packets left in the previous chain.
	s.InputSyncer.Flags.Set(barrierstategetter.FlagSwitchFirstPacketAfterSwitchPassBothOutputs)
	s.InputSyncer.SetOnAfterSwitch(func(ctx context.Context, in packetorframe.InputUnion, from, to int32) {
		prevDataLocker.Lock()
		prevSource = in.Packet.Source
		prevOutputID = from
		logger.Debugf(ctx, "s.OutputSyncFilter.SetValue(ctx, %d)", to)
		err := s.OutputSyncer.SetValue(ctx, to)
		logger.Debugf(ctx, "/s.OutputSyncFilter.SetValue(ctx, %d): %v", to, err)
		outputSyncerSwitchDeadline = time.Now().Add(10 * time.Second)
		// this will be our notification packet (that nothing is left in the previous chain):
		in.Packet.PipelineSideData = append(in.Packet.PipelineSideData, switchNotifierPacket{
			SwitchFromOutputID: from,
		})
	})

	// Waiting for the "last packet" notification to reach the OutputSyncer, since
	// we want to make the switch only after the last packet in the chain is processed.
	//
	// But just in case we also have a 10 seconds timeout (in case the packet was
	// consumed by something in the chain somehow) -- this is the last resort thing,
	// it theoretically should never happen.
	s.OutputSyncer.Flags.Set(barrierstategetter.FlagSwitchForbidTakeoverInKeepUnless)
	s.OutputSyncer.SetKeepUnless(
		packetorframecondition.And{
			packetorframecondition.Or{
				packetorframecondition.And{
					packetorframecondition.PacketSource(prevSource),
					packetorframecondition.HasPipelineSideData(switchNotifierPacket{
						SwitchFromOutputID: prevOutputID,
					}),
				},
				packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
					if time.Since(outputSyncerSwitchDeadline) <= 0 {
						return false
					}
					logger.Errorf(ctx, "OutputSyncer: timeout waiting for the last packet to reach the OutputSyncer")
					return true
				}),
			},
			packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
				logger.Debugf(ctx, "unlocking prevDataLocker")
				prevDataLocker.Unlock()
				return true
			}),
		},
	)
	s.OutputSyncer.SetOnAfterSwitch(func(ctx context.Context, in packetorframe.InputUnion, from, to int32) {
		logger.Debugf(ctx, "releasing OutputSyncer")
		s.OutputSyncer.Release()
	})
	return s, nil
}

func (s *StreamMux[C]) InitOutput(
	ctx context.Context,
	outputKey types.OutputKey,
	opts ...InitOutputOption,
) (output *Output[C], _err error) {
	logger.Tracef(ctx, "InitOutput(%#+v)", outputKey)
	defer func() { logger.Tracef(ctx, "/InitOutput(%#+v): %v", outputKey, _err) }()
	return xsync.DoA3R2(ctx, &s.Locker, s.initOutputLocked, ctx, outputKey, opts)
}

func (s *StreamMux[C]) initOutputLocked(
	ctx context.Context,
	outputKey types.OutputKey,
	opts []InitOutputOption,
) (_ret *Output[C], _err error) {
	if _, ok := s.OutputsMap[outputKey]; ok {
		return
	}

	outputID := len(s.Outputs)
	output, err := newOutput(
		ctx,
		outputID,
		s.Input(),
		s.OutputFactory, outputKey,
		s.InputSyncer.Condition(int32(outputID)),
		s.OutputSyncer.Condition(int32(outputID)),
		newStreamIndexAssigner(s.MuxMode, outputID, s.InputNode.Processor.Kernel),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output: %w", err)
	}
	s.Outputs = append(s.Outputs, output)
	s.OutputsMap[outputKey] = output
	s.Input().AddPushPacketsTo(
		output.Input(),
	)
	return output, nil
}

func (s *StreamMux[C]) Input() *InputNode[C] {
	return s.InputNode
}

func (s *StreamMux[C]) GetRecoderConfig(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	logger.Tracef(ctx, "GetRecoderConfig")
	defer func() { logger.Tracef(ctx, "/GetRecoderConfig: %v", _ret) }()
	if s == nil {
		return types.RecoderConfig{}
	}
	return xsync.DoA1R1(ctx, &s.Locker, s.getRecoderConfigLocked, ctx)
}

func (s *StreamMux[C]) getRecoderConfigLocked(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	return s.RecodingConfig
}

func (s *StreamMux[C]) SetRecoderConfig(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "SetRecoderConfig(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/SetRecoderConfig(ctx, %#+v): %v", cfg, _err) }()
	return xsync.DoA2R1(ctx, &s.Locker, s.setRecoderConfigLocked, ctx, cfg)
}

func (s *StreamMux[C]) setRecoderConfigLocked(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	outputKey := cfg.OutputKey()
	output := s.OutputsMap[outputKey]
	if output == nil {
		logger.Warnf(ctx, "no output with key %s found, initializing it; but please call InitOutput, instead of relying on this automation", outputKey)
		var err error
		output, err = s.initOutputLocked(ctx, outputKey, nil)
		if err != nil {
			return fmt.Errorf("unable to initialize the output %s: %w", outputKey, err)
		}
	}

	logger.Debugf(ctx, "reconfiguring the output %d:%s", output.ID, outputKey)
	return s.reconfigureOutput(ctx, output, cfg)
}

func (s *StreamMux[C]) configureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "configureRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/configureRecoder(ctx, %#+v): %v", cfg, _err) }()
	if len(cfg.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.VideoTrackConfigs))
	}
	if len(cfg.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output audio track config (received a request for %d track configs)", len(cfg.AudioTrackConfigs))
	}
	if s.Recoder == nil {
		if err := s.initRecoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to initialize the recoder: %w", err)
		}
		return nil
	}
	if cfg.AudioTrackConfigs[0].CodecName != codec.CodecNameCopy {
		return fmt.Errorf("we currently do not support audio recoding: '%s' != 'copy'", cfg.AudioTrackConfigs[0].CodecName)
	}
	if cfg.VideoTrackConfigs[0].CodecName == codec.CodecNameCopy {
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

func (s *StreamMux[C]) initRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "initRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/initRecoder(ctx, %#+v): %v", cfg, _err) }()

	if s.Recoder != nil {
		return fmt.Errorf("internal error: an encoder is already initialized")
	}

	var (
		videoQuality    codec.Quality
		videoResolution *codec.Resolution
	)

	if bitRate := cfg.VideoTrackConfigs[0].AverageBitRate; bitRate > 0 {
		videoQuality = quality.ConstantBitrate(bitRate)
	}

	if width, height := cfg.VideoTrackConfigs[0].Width, cfg.VideoTrackConfigs[0].Height; width > 0 && height > 0 {
		videoResolution = &codec.Resolution{
			Width:  width,
			Height: height,
		}
	}

	var err error
	s.Recoder, err = kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx,
			&codec.NaiveDecoderFactoryParams{
				HardwareDeviceType: avptypes.HardwareDeviceType(cfg.VideoTrackConfigs[0].HardwareDeviceType),
				HardwareDeviceName: avptypes.HardwareDeviceName(cfg.VideoTrackConfigs[0].HardwareDeviceName),
				PostInitFunc: func(ctx context.Context, d *codec.Decoder) {
					err := d.SetLowLatency(ctx, true)
					if err != nil {
						logger.Warnf(ctx, "unable to enable the low latency mode on the decoder: %v", err)
					}
				},
			},
		),
		codec.NewNaiveEncoderFactory(ctx,
			&codec.NaiveEncoderFactoryParams{
				VideoCodec:         cfg.VideoTrackConfigs[0].CodecName,
				AudioCodec:         codec.CodecNameCopy,
				HardwareDeviceType: avptypes.HardwareDeviceType(cfg.VideoTrackConfigs[0].HardwareDeviceType),
				HardwareDeviceName: avptypes.HardwareDeviceName(cfg.VideoTrackConfigs[0].HardwareDeviceName),
				VideoOptions:       convertCustomOptions(cfg.VideoTrackConfigs[0].CustomOptions).ToAstiav(),
				AudioOptions:       convertCustomOptions(cfg.AudioTrackConfigs[0].CustomOptions).ToAstiav(),
				VideoQuality:       videoQuality,
				VideoResolution:    videoResolution,
			},
		),
		nil,
	)

	if err != nil {
		return fmt.Errorf("unable to initialize a recoder: %w", err)
	}
	return nil
}

func (s *StreamMux[C]) reconfigureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureRecoder(ctx, %#+v): %v", cfg, _err) }()

	encoderFactory := s.Recoder.EncoderFactory
	if cfg.VideoTrackConfigs[0].CodecName != encoderFactory.VideoCodec {
		return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", cfg.VideoTrackConfigs[0].CodecName, encoderFactory.VideoCodec)
	}

	err := xsync.DoR1(ctx, &s.Recoder.EncoderFactory.Locker, func() error {
		videoCfg := cfg.VideoTrackConfigs[0]
		if len(s.Recoder.EncoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			if s.Recoder.EncoderFactory.VideoOptions == nil {
				s.Recoder.EncoderFactory.VideoOptions = astiav.NewDictionary()
				setFinalizerFree(ctx, s.Recoder.EncoderFactory.VideoOptions)
			}

			if videoCfg.AverageBitRate == 0 {
				s.Recoder.EncoderFactory.VideoOptions.Unset("b")
			} else {
				s.Recoder.EncoderFactory.VideoOptions.Set("b", fmt.Sprintf("%d", videoCfg.AverageBitRate), 0)
			}
			return nil
		}

		logger.Debugf(ctx, "the encoder is already initialized, so modifying it if needed")
		encoder := s.Recoder.EncoderFactory.VideoEncoders[0]

		{
			q := encoder.GetQuality(ctx)
			if q == nil {
				logger.Errorf(ctx, "unable to get the current encoding quality")
				q = quality.ConstantBitrate(0)
			}
			logger.Debugf(ctx, "current quality: %#+v; requested quality: %#+v", q, quality.ConstantBitrate(videoCfg.AverageBitRate))

			needsChangingBitrate := true
			if q, ok := q.(quality.ConstantBitrate); ok {
				if q == quality.ConstantBitrate(videoCfg.AverageBitRate) {
					needsChangingBitrate = false
				}
			}

			if needsChangingBitrate && videoCfg.AverageBitRate > 0 {
				logger.Debugf(ctx, "bitrate needs changing...")
				err := encoder.SetQuality(ctx, quality.ConstantBitrate(videoCfg.AverageBitRate), nil)
				if err != nil {
					return fmt.Errorf("unable to set bitrate to %v: %w", videoCfg.AverageBitRate, err)
				}
			}
		}

		{
			w, h := encoder.GetResolution(ctx)
			logger.Debugf(ctx, "current resolution: %dx%d; requested resolution: %dx%d", w, h)
			if w != videoCfg.Width && h != videoCfg.Height {
				logger.Debugf(ctx, "resolution needs changing...")
				err := encoder.SetResolution(ctx, videoCfg.Width, videoCfg.Height, nil)
				if err != nil {
					return fmt.Errorf("unable to set resolution to %d%x: %w", videoCfg.Width, videoCfg.Height, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = s.SwitchPreFilter.SetValue(ctx, 0)
	if err != nil {
		return fmt.Errorf("unable to switch the pre-filter to recoding: %w", err)
	}

	return nil
}

func (s *StreamMux[C]) reconfigureRecoderCopy(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureRecoderCopy(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureRecoderCopy(ctx, %#+v): %v", cfg, _err) }()

	switchState := s.SwitchPreFilter.CurrentValue.Load()

	err := s.SwitchPreFilter.SetValue(ctx, 1)
	if err != nil {
		return fmt.Errorf("unable to switch the pre-filter to passthrough: %w", err)
	}
	s.FilterThrottle.BitrateAveragingPeriod = cfg.VideoTrackConfigs[0].AveragingPeriod
	s.FilterThrottle.AverageBitRate = cfg.VideoTrackConfigs[0].AverageBitRate // if AverageBitRate != 0 then here we also enable the throttler (if it was disabled)

	if switchState == 0 { // was in the recoding state
		// to avoid the recoder sending some packets from an obsolete state (when we are going to reuse it), we just reset it.
		if err := s.Recoder.Reset(ctx); err != nil {
			logger.Errorf(ctx, "unable to reset the recoder: %v", err)
		}
	}

	return nil
}

func (s *StreamMux[C]) GetAllStats(
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
	tryGetStats("Input", s.MapInputStreamIndicesNode)
	for idx, output := range s.OutputsMap {
		tryGetStats(fmt.Sprintf("Output%d", idx), output)
	}
	return m
}

func asPacketSource(proc processor.Abstract) packet.Source {
	if getPacketSourcer, ok := proc.(processor.GetPacketSourcer); ok {
		if packetSource := getPacketSourcer.GetPacketSource(); packetSource != nil {
			return packetSource
		}
	}
	return nil
}

func asPacketSink(proc processor.Abstract) packet.Sink {
	if getPacketSinker, ok := proc.(processor.GetPacketSinker); ok {
		if packetSink := getPacketSinker.GetPacketSink(); packetSink != nil {
			return packetSink
		}
	}
	return nil
}

func (s *StreamMux[C]) Start(
	ctx context.Context,
	passthroughMode types.MuxMode,
	serveCfg avpipeline.ServeConfig,
) (_err error) {
	logger.Debugf(ctx, "Start(ctx, %s)", passthroughMode)
	defer logger.Debugf(ctx, "/Start(ctx, %s): %v", passthroughMode, _err)
	if s.Recoder == nil {
		return fmt.Errorf("s.Recoder is not configured")
	}
	outputMain := &nodewrapper.NoServe[node.Abstract]{Node: s.OutputsMap[0]}
	outputAsPacketSink := asPacketSink(outputMain.GetProcessor())
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

	if passthroughMode != types.MuxModeForbid {
		s.MapInputStreamIndicesNode.AddPushPacketsTo(
			s.NodeRecoder,
			packetfiltercondition.Packet{
				Condition: packetcondition.And{
					s.SwitchPreFilter.PacketCondition(0),
					s.SwitchPostFilter.PacketCondition(0),
				},
			},
		)
		s.MapInputStreamIndicesNode.AddPushPacketsTo(
			nodeFilterThrottle,
			packetfiltercondition.Packet{
				Condition: packetcondition.And{
					s.SwitchPreFilter.PacketCondition(1),
					s.SwitchPostFilter.PacketCondition(1),
				},
			},
		)

		if notifyAboutPacketSources {
			logger.Debugf(ctx, "notifying about the sources (the first time)")
			err := avpipeline.NotifyAboutPacketSources(ctx, s.PacketSource, s.MapInputStreamIndicesNode)
			if err != nil {
				return fmt.Errorf("received an error while notifying nodes about packet sources the first time (%s -> %s): %w", s.PacketSource, s.MapInputStreamIndicesNode, err)
			}
		}
	} else {
		s.MapInputStreamIndicesNode.AddPushPacketsTo(s.NodeRecoder)
		s.MapInputStreamIndicesNode.AddPushFramesTo(s.NodeRecoder)
	}

	s.NodeStreamFixerMain = autofix.New(
		belt.WithField(ctx, "branch", "output-main"),
		s.Recoder.Encoder,
		outputAsPacketSink,
	)

	if passthroughMode != types.MuxModeForbid {
		var passthroughOutputReference packet.Sink = s.Recoder.Decoder
		if passthroughMode == types.MuxModeDifferentOutputsSameTracks {
			passthroughOutputReference = asPacketSink(s.OutputsMap[1].GetProcessor())
		}
		s.NodeStreamFixerPassthrough = autofix.New(
			belt.WithField(ctx, "branch", "passthrough"),
			s.PacketSource,
			passthroughOutputReference,
		)
		if s.NodeStreamFixerPassthrough != nil {
			nodeFilterThrottle.AddPushPacketsTo(s.NodeStreamFixerPassthrough)
			nodeFilterThrottle.AddPushFramesTo(s.NodeStreamFixerPassthrough)
		}

		if startWithPassthrough {
			s.SwitchPreFilter.CurrentValue.Store(1)
			s.SwitchPostFilter.CurrentValue.Store(1)
			s.SwitchPreFilter.NextValue.Store(1)
			s.SwitchPostFilter.NextValue.Store(1)
		}

		if passthroughRescaleTS {
			if !startWithPassthrough || notifyAboutPacketSources {
				nodeFilterThrottle.InputPacketFilter = packetfiltercondition.Packet{
					Condition: filter.NewRescaleTSBetweenKernels(
						s.PacketSource,
						s.NodeRecoder.Processor.Kernel.Encoder,
					),
				}
			} else {
				logger.Warnf(ctx, "unable to configure rescale_ts because startWithPassthrough && !notifyAboutPacketSources")
			}
		}

		var condRecoder, condPassthrough packetfiltercondition.And
		var sinkMain, sinkPassthrough node.Abstract
		switch passthroughMode {
		case types.MuxModeSameOutputSameTracks:
			condRecoder = append(condRecoder, packetfiltercondition.Packet{
				Condition: packetcondition.And{
					s.SwitchPreFilter.PacketCondition(0),
					s.SwitchPostFilter.PacketCondition(0),
				},
			})
			condPassthrough = append(condPassthrough, packetfiltercondition.Packet{
				Condition: packetcondition.And{
					s.SwitchPreFilter.PacketCondition(1),
					s.SwitchPostFilter.PacketCondition(1),
				},
			})
			sinkMain, sinkPassthrough = outputMain, s.NodeStreamFixerMain
			if s.NodeStreamFixerMain == nil {
				sinkPassthrough = outputMain
			}
		case types.MuxModeSameOutputDifferentTracks:
			nodeMapStreamIndices := node.NewFromKernel(
				ctx,
				s.MapOutputStreamIndices,
				processor.DefaultOptionsOutput()...,
			)
			nodeMapStreamIndices.AddPushPacketsTo(outputMain)
			sinkMain, sinkPassthrough = nodeMapStreamIndices, s.NodeStreamFixerMain
			if s.NodeStreamFixerMain == nil {
				sinkPassthrough = nodeMapStreamIndices
			}
		case types.MuxModeDifferentOutputsSameTracks:
			condRecoder = append(condRecoder, packetfiltercondition.Packet{
				Condition: packetcondition.And{
					s.SwitchPreFilter.PacketCondition(0),
					s.SwitchPostFilter.PacketCondition(0),
				},
			})
			condPassthrough = append(condPassthrough, packetfiltercondition.Packet{
				Condition: packetcondition.And{
					s.SwitchPreFilter.PacketCondition(1),
					s.SwitchPostFilter.PacketCondition(1),
				},
			})
			sinkMain, sinkPassthrough = outputMain, &nodewrapper.NoServe[node.Abstract]{Node: s.OutputsMap[1]}
		default:
			return fmt.Errorf("unknown passthrough mode: '%s'", passthroughMode)
		}
		if s.NodeStreamFixerMain != nil {
			s.NodeRecoder.AddPushPacketsTo(s.NodeStreamFixerMain, condRecoder...)
			s.NodeStreamFixerMain.AddPushPacketsTo(sinkMain)
		} else {
			s.NodeRecoder.AddPushPacketsTo(sinkMain, condRecoder...)
		}
		if s.NodeStreamFixerPassthrough != nil {
			s.NodeStreamFixerPassthrough.AddPushPacketsTo(sinkPassthrough, condPassthrough...)
		} else {
			nodeFilterThrottle.AddPushPacketsTo(sinkPassthrough, condPassthrough...)
		}
	} else {
		if s.NodeStreamFixerMain != nil {
			s.NodeRecoder.AddPushPacketsTo(s.NodeStreamFixerMain)
			s.NodeRecoder.AddPushFramesTo(s.NodeStreamFixerMain)
			s.NodeStreamFixerMain.AddPushPacketsTo(outputMain)
			s.NodeStreamFixerMain.AddPushFramesTo(outputMain)
		} else {
			s.NodeRecoder.AddPushPacketsTo(outputMain)
			s.NodeRecoder.AddPushFramesTo(outputMain)
		}
	}

	// == spawn an observer ==

	errCh := make(chan node.Error, 100)
	s.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
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
		logger.Debugf(ctx, "notifying about the sources")
		err := avpipeline.NotifyAboutPacketSources(ctx, s.PacketSource, s.MapInputStreamIndicesNode)
		if err != nil {
			return fmt.Errorf("received an error while notifying nodes about packet sources (%s -> %s): %w", s.PacketSource, s.MapInputStreamIndicesNode, err)
		}
	}
	logger.Debugf(ctx, "resulting graph: %s", node.Nodes[node.Abstract]{s.MapInputStreamIndicesNode}.StringRecursive())
	logger.Debugf(ctx, "resulting graph (graphviz): %s", node.Nodes[node.Abstract]{s.MapInputStreamIndicesNode}.DotString(false))

	// == launch ==

	s.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer s.waitGroup.Done()
		defer cancelFn()
		defer logger.Debugf(ctx, "finished the serving routine")

		avpipeline.Serve(ctx, serveCfg, errCh, s.MapInputStreamIndicesNode)
	})

	return nil
}

func (s *StreamMux[C]) Wait(
	ctx context.Context,
) error {
	s.waitGroup.Wait()
	return nil
}
