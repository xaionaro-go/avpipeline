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
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	passthroughRescaleTS     = false
	notifyAboutPacketSources = true
	startWithPassthrough     = false
)

type NodeInput[C any] = node.NodeWithCustomData[
	C, *processor.FromKernel[*boilerplate.BaseWithFormatContext[*InputHandler]],
]

type StreamMux[C any] struct {
	MuxMode        types.MuxMode
	RecodingConfig types.RecoderConfig
	Locker         xsync.Mutex

	// switches:
	OutputSwitch *barrierstategetter.Switch
	OutputSyncer *barrierstategetter.Switch

	// nodes:
	InputNode     *NodeInput[C]
	Outputs       []*Output[C]
	OutputsMap    map[OutputKey]*Output[C]
	OutputFactory OutputFactory

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
		OutputSwitch:  barrierstategetter.NewSwitch(),
		OutputSyncer:  barrierstategetter.NewSwitch(),
		OutputFactory: outputFactory,
		OutputsMap:    map[OutputKey]*Output[C]{},
	}

	s.OutputSwitch.SetKeepUnless(packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.IsKeyFrame(true),
	})
	var (
		prevDataLocker             sync.Mutex
		prevOutputID               int32
		outputSyncerSwitchDeadline time.Time
	)

	type switchNotifierPacket struct {
		SwitchFromOutputID int32
	}

	// We want to notify the OutputSyncer that it needs to switch to the next output, and
	// send a packet that would notify that there are no packets left in the previous chain.
	s.OutputSwitch.Flags.Set(barrierstategetter.SwitchFlagFirstPacketAfterSwitchPassBothOutputs)
	s.OutputSwitch.SetOnAfterSwitch(func(ctx context.Context, in packetorframe.InputUnion, from, to int32) {
		prevDataLocker.Lock()
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

	// Waiting for the "last packet" notification to reach the OutputSyncer on the active
	// output chain, since we want to make the switch only after the last packet in the
	// chain is processed.
	//
	// But just in case we also have a 10 seconds timeout (in case the packet was
	// consumed by something in the chain somehow) -- this is the last resort thing,
	// it theoretically should never happen.
	s.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagForbidTakeoverInKeepUnless)
	s.OutputSyncer.SetKeepUnless(
		packetorframecondition.And{
			packetorframecondition.Or{
				packetorframecondition.HasPipelineSideData(switchNotifierPacket{
					SwitchFromOutputID: prevOutputID,
				}),
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
	// keep/hold the traffic flowing to the next output if the OutputSyncer is not switched yet
	s.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagNextOutputStateBlock)
	return s, nil
}

func (s *StreamMux[C]) setPreferredOutput(
	ctx context.Context,
	outputKey OutputKey,
) (_err error) {
	logger.Tracef(ctx, "setPreferredOutput(ctx, %s)", outputKey)
	defer func() { logger.Tracef(ctx, "/setPreferredOutput(ctx, %s): %v", outputKey, _err) }()

	output := s.OutputsMap[outputKey]
	err := s.OutputSwitch.SetValue(ctx, int32(output.ID))
	if err != nil {
		return fmt.Errorf("unable to set the preferred output %d:%s: %w", output.ID, outputKey, err)
	}

	return nil
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
		s.OutputSwitch.Output(int32(outputID)),
		s.OutputSyncer.Output(int32(outputID)),
		newStreamIndexAssigner(s.MuxMode, outputID, s.InputNode.Processor.Kernel),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output: %w", err)
	}
	s.Outputs = append(s.Outputs, output)
	s.OutputsMap[outputKey] = output
	s.Input().AddPushPacketsTo(output.Input())
	return output, nil
}

func (s *StreamMux[C]) Input() *NodeInput[C] {
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
	err := s.reconfigureOutput(ctx, output, cfg)
	if err != nil {
		return fmt.Errorf("unable to reconfigure the output %d:%s: %w", output.ID, outputKey, err)
	}

	err = s.setPreferredOutput(ctx, outputKey)
	if err != nil {
		return fmt.Errorf("unable to set the preferred output %d:%s: %w", output.ID, outputKey, err)
	}

	return nil
}

func (s *StreamMux[C]) reconfigureOutput(
	ctx context.Context,
	output *Output[C],
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureOutput(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureOutput(ctx, %#+v): %v", cfg, _err) }()

	if len(cfg.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.VideoTrackConfigs))
	}
	videoCfg := cfg.VideoTrackConfigs[0]

	if len(cfg.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output audio track config (received a request for %d track configs)", len(cfg.AudioTrackConfigs))
	}
	audioCfg := cfg.VideoTrackConfigs[0]

	encoderFactory := output.RecoderNode.Processor.Kernel.EncoderFactory
	if videoCfg.CodecName != encoderFactory.VideoCodec {
		return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", videoCfg.CodecName, encoderFactory.VideoCodec)
	}

	err := xsync.DoR1(ctx, &encoderFactory.Locker, func() error {
		if len(encoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			encoderFactory.AudioOptions = convertCustomOptions(audioCfg.CustomOptions).ToAstiav()
			encoderFactory.VideoOptions = convertCustomOptions(videoCfg.CustomOptions).ToAstiav()
			encoderFactory.HardwareDeviceName = codec.HardwareDeviceName(videoCfg.HardwareDeviceName)
			encoderFactory.HardwareDeviceType = astiav.HardwareDeviceType(videoCfg.HardwareDeviceType)
			if videoCfg.AverageBitRate != 0 {
				encoderFactory.VideoQuality = quality.ConstantBitrate(videoCfg.AverageBitRate)
			}
			return nil
		}

		logger.Debugf(ctx, "the encoder is already initialized, so modifying it if needed")
		encoder := encoderFactory.VideoEncoders[0]

		if videoCfg.HardwareDeviceType != types.HardwareDeviceType(encoderFactory.HardwareDeviceType) {
			return fmt.Errorf("unable to change the hardware device type on the fly, yet: '%s' != '%s'", videoCfg.HardwareDeviceType, encoderFactory.HardwareDeviceType)
		}

		if videoCfg.HardwareDeviceName != types.HardwareDeviceName(encoderFactory.HardwareDeviceName) {
			return fmt.Errorf("unable to change the hardware device name on the fly, yet: '%s' != '%s'", videoCfg.HardwareDeviceName, encoderFactory.HardwareDeviceName)
		}

		{
			q := encoder.GetQuality(ctx)
			if q == nil {
				logger.Errorf(ctx, "unable to get the current encoding quality")
				q = quality.ConstantBitrate(0)
			}
			logger.Debugf(ctx,
				"current quality: %#+v; requested quality: %#+v",
				q, quality.ConstantBitrate(videoCfg.AverageBitRate),
			)

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
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *StreamMux[C]) GetAllStats(
	ctx context.Context,
) map[string]*node.ProcessingStatistics {
	return xsync.DoA1R1(ctx, &s.Locker, s.getAllStatsLocked, ctx)
}

func (s *StreamMux[C]) getAllStatsLocked(
	ctx context.Context,
) map[string]*node.ProcessingStatistics {
	m := map[string]*node.ProcessingStatistics{}
	tryGetStats := func(key string, n node.Abstract) {
		getter, ok := n.(interface {
			GetStats() *node.ProcessingStatistics
		})
		if !ok {
			return
		}
		m[key] = getter.GetStats()
	}
	tryGetStats("Input", s.InputNode)
	for outputKey, output := range s.OutputsMap {
		tryGetStats(fmt.Sprintf("Output(%s):InputFilter", outputKey), output.InputFilter)
		tryGetStats(fmt.Sprintf("Output(%s):InputFixer", outputKey), output.InputFixer)
		tryGetStats(fmt.Sprintf("Output(%s):RecoderNode", outputKey), output.RecoderNode)
		tryGetStats(fmt.Sprintf("Output(%s):MapIndexes", outputKey), output.MapIndices)
		tryGetStats(fmt.Sprintf("Output(%s):OutputFixer", outputKey), output.OutputFixer)
		tryGetStats(fmt.Sprintf("Output(%s):OutputSyncFilter", outputKey), output.OutputSyncFilter)
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
