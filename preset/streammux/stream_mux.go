package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	nodeboilerplate "github.com/xaionaro-go/avpipeline/node/boilerplate"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

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

	// aux
	InputNodeAsPacketSource packet.Source

	// to become a fake node myself:
	nodeboilerplate.Statistics
	nodeboilerplate.InputFilter

	startCh   *chan struct{}
	waitGroup sync.WaitGroup
}

type OutputFactory interface {
	NewOutput(
		ctx context.Context,
		outputKey OutputKey,
	) (node.Abstract, OutputConfig, error)
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
		InputNode:     newInputNode[C](ctx),
		OutputSwitch:  barrierstategetter.NewSwitch(),
		OutputSyncer:  barrierstategetter.NewSwitch(),
		OutputFactory: outputFactory,
		OutputsMap:    map[OutputKey]*Output[C]{},
		MuxMode:       muxMode,

		startCh: ptr(make(chan struct{})),
	}
	s.InputNodeAsPacketSource = s.InputNode.Processor.GetPacketSource()
	s.initSwitches()
	return s, nil
}

func (s *StreamMux[C]) initSwitches() {
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
	// output chain, since we want to make the final switch only after the last packet in the
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
			// unlocking prevDataLocker if ready to switch to the next output
			packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
				logger.Debugf(ctx, "unlocking prevDataLocker")
				prevDataLocker.Unlock()
				return true
			}),
		},
	)
	// keep/hold the traffic flowing to the next output if the OutputSyncer is not switched yet
	s.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagNextOutputStateBlock)
}

func (p *StreamMux[C]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "StreamMux.Close()")
	defer func() { logger.Debugf(ctx, "/StreamMux.Close(): %v", _err) }()
	var errs []error

	if err := p.InputNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close the input node: %w", err))
	}
	for _, output := range p.Outputs {
		if err := output.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the output chain %d: %w", output.ID, err))
		}
	}
	return errors.Join(errs...)
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
	return xsync.DoA3R2(ctx, &s.Locker, s.getOrInitOutputLocked, ctx, outputKey, opts)
}

func (s *StreamMux[C]) getOrInitOutputLocked(
	ctx context.Context,
	outputKey types.OutputKey,
	opts []InitOutputOption,
) (_ret *Output[C], _err error) {
	if output, ok := s.OutputsMap[outputKey]; ok {
		return output, nil
	}
	switch s.MuxMode {
	case types.UndefinedMuxMode:
		return nil, fmt.Errorf("mux mode is not defined")
	case types.MuxModeForbid:
		if len(s.Outputs) > 0 {
			return nil, fmt.Errorf("mux mode %s forbids adding new outputs, but already have %d outputs", s.MuxMode, len(s.Outputs))
		}
	case types.MuxModeSameOutputSameTracks, types.MuxModeSameOutputDifferentTracks:
		if len(s.Outputs) > 0 {
			return s.Outputs[0], nil
		}
	}

	cfg := InitOutputOptions(opts).config()
	if cfg.RetryParameters != nil {
		return nil, fmt.Errorf("retry parameters are not supported in the StreamMux preset, yet")
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
	err = avpipeline.NotifyAboutPacketSources(ctx, s.InputNodeAsPacketSource, output.Input())
	if err != nil {
		logger.Errorf(ctx, "received an error while notifying about packet sources (%s -> %s): %v", s.InputNodeAsPacketSource, output.Input(), err)
	}
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
	outputKey := cfg.OutputKey(ctx)
	var err error
	output, err := s.getOrInitOutputLocked(ctx, outputKey, nil)
	if err != nil {
		return fmt.Errorf("unable to get-or-initialize the output %s: %w", outputKey, err)
	}

	logger.Debugf(ctx, "reconfiguring the output %d:%s", output.ID, outputKey)
	err = s.reconfigureOutput(ctx, output, cfg)
	if err != nil {
		return fmt.Errorf("unable to reconfigure the output %d:%s: %w", output.ID, outputKey, err)
	}

	logger.Debugf(ctx, "notifying about the sources")
	err = avpipeline.NotifyAboutPacketSources(ctx,
		s.InputNodeAsPacketSource,
		output.InputFilter,
	)
	if err != nil {
		return fmt.Errorf("received an error while notifying nodes about packet sources (%s -> %s): %w", s.InputNodeAsPacketSource, output.InputFilter, err)
	}

	err = s.setPreferredOutput(ctx, outputKey)
	if err != nil {
		return fmt.Errorf("unable to set the preferred output %d:%s: %w", output.ID, outputKey, err)
	}

	s.RecodingConfig = cfg
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

	err := xsync.DoR1(ctx, &encoderFactory.Locker, func() error {
		if len(encoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			encoderFactory.VideoCodec = videoCfg.CodecName
			encoderFactory.AudioCodec = audioCfg.CodecName
			encoderFactory.AudioOptions = convertCustomOptions(audioCfg.CustomOptions).ToAstiav()
			encoderFactory.VideoOptions = convertCustomOptions(videoCfg.CustomOptions).ToAstiav()
			encoderFactory.HardwareDeviceName = codec.HardwareDeviceName(videoCfg.HardwareDeviceName)
			encoderFactory.HardwareDeviceType = astiav.HardwareDeviceType(videoCfg.HardwareDeviceType)
			if videoCfg.AverageBitRate != 0 {
				encoderFactory.VideoQuality = quality.ConstantBitrate(videoCfg.AverageBitRate)
			}
			return nil
		}

		if videoCfg.CodecName != encoderFactory.VideoCodec {
			return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", videoCfg.CodecName, encoderFactory.VideoCodec)
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
	_ context.Context,
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
		tryGetStats(fmt.Sprintf("Output(%s):OutputSyncFilter", outputKey), output.OutputSyncer)
	}
	return m
}

func (s *StreamMux[C]) Start(
	ctx context.Context,
	serveCfg node.ServeConfig,
) (_err error) {
	logger.Debugf(ctx, "Start(ctx)")
	defer logger.Debugf(ctx, "/Start(ctx): %v", _err)

	ctx, cancelFn := context.WithCancel(ctx)

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

	logger.Debugf(ctx, "resulting graph: %s", node.Nodes[node.Abstract]{s.InputNode}.StringRecursive())
	logger.Debugf(ctx, "resulting graph (graphviz): %s", node.Nodes[node.Abstract]{s.InputNode}.DotString(false))

	// == launch ==

	s.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer s.waitGroup.Done()
		defer cancelFn()
		defer logger.Debugf(ctx, "finished the serving routine")

		s.Serve(ctx, serveCfg, errCh)
	})

	return nil
}

func (s *StreamMux[C]) WaitForStartChan() <-chan struct{} {
	return *xatomic.LoadPointer(&s.startCh)
}

func (s *StreamMux[C]) WaitForStart(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "WaitForStart")
	defer func() { logger.Tracef(ctx, "/WaitForStart: %v", _err) }()
	select {
	case <-s.WaitForStartChan():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *StreamMux[C]) WaitForStop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "WaitForStop")
	defer func() { logger.Tracef(ctx, "/WaitForStop: %v", _err) }()
	s.waitGroup.Wait()
	return nil
}
