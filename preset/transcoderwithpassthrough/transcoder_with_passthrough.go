package transcoderwithpassthrough

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packet/filter/rescaletsbetweenpacketsources"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/quality"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	xastiav "github.com/xaionaro-go/avpipeline/types/astiav"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	passthroughRescaleTS     = false
	notifyAboutPacketSources = true
	startWithPassthrough     = false
)

// TODO: delete me after `streammux` is productionized.
type TranscoderWithPassthrough[C any, P processor.Abstract] struct {
	PacketSource              packet.Source
	Outputs                   []node.Abstract
	FilterThrottle            *packetcondition.VideoAverageBitrateLower
	SwitchPreFilter           *packetcondition.Switch
	SwitchPostFilter          *packetcondition.Switch
	Recoder                   *kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	MapInputStreamIndicesNode *node.Node[*processor.FromKernel[*kernel.MapStreamIndices]]
	MapOutputStreamIndices    *kernel.MapStreamIndices
	NodeRecoder               *node.Node[*processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]

	NodeStreamFixerMain        *autofix.AutoFixer
	NodeStreamFixerPassthrough *autofix.AutoFixer

	RecodingConfig types.RecoderConfig

	inputStreamMapIndicesAsPacketSource packet.Source

	locker    xsync.Mutex
	waitGroup sync.WaitGroup
}

/*
// 1 output (same_tracks, same_connection)
//
//                           +--> THROTTLE --> AutoFixer ->--+
// INPUT --> MAP INDICES ->--+               (passthrough)   +-> AutoFixer (--> MAP INDICES) --> OUTPUT-main
//                           +--> RECODER -------------------+    (main)   [same_connection]
*/

/*
// 2 outputs (next_output):
//
//                           +--> THROTTLE ---> AutoFixer --> OUTPUT-passthrough
// INPUT --> MAP INDICES ->--+                (passthrough)
//                           +--> RECODER ----> AutoFixer --> OUTPUT-main
//                                               (main)
*/
func New[C any, P processor.Abstract](
	ctx context.Context,
	input packet.Source,
	outputs ...node.Abstract,
) (*TranscoderWithPassthrough[C, P], error) {
	s := &TranscoderWithPassthrough[C, P]{
		PacketSource:     input,
		Outputs:          outputs,
		FilterThrottle:   packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		SwitchPreFilter:  packetcondition.NewSwitch(),
		SwitchPostFilter: packetcondition.NewSwitch(),
	}
	s.SwitchPreFilter.SetKeepUnless(packetcondition.And{
		packetcondition.MediaType(astiav.MediaTypeVideo),
		packetcondition.IsKeyFrame(true),
	})
	s.SwitchPreFilter.SetOnAfterSwitch(func(ctx context.Context, pkt packet.Input, from, to int32) {
		logger.Debugf(ctx, "s.PostSwitchFilter.SetValue(ctx, %d)", to)
		err := s.SwitchPostFilter.SetValue(ctx, to)
		logger.Debugf(ctx, "/s.PostSwitchFilter.SetValue(ctx, %d): %v", to, err)
	})
	s.SwitchPostFilter.SetKeepUnless(packetcondition.And{
		packetcondition.MediaType(astiav.MediaTypeVideo),
		packetcondition.IsKeyFrame(true),
	})
	s.MapInputStreamIndicesNode = node.NewFromKernel(
		ctx,
		kernel.NewMapStreamIndices(ctx, newStreamIndexAssignerInput(ctx, s)),
		processor.OptionQueueSizeInputPacket(6000),
		processor.OptionQueueSizeInputFrame(6000),
		processor.OptionQueueSizeOutputPacket(10),
		processor.OptionQueueSizeOutputFrame(10),
		processor.OptionQueueSizeError(2),
	)
	s.inputStreamMapIndicesAsPacketSource = asPacketSource(s.MapInputStreamIndicesNode.Processor)
	s.MapOutputStreamIndices = kernel.NewMapStreamIndices(ctx, newStreamIndexAssignerOutput(s))
	return s, nil
}

func (s *TranscoderWithPassthrough[C, P]) Input() *node.Node[*processor.FromKernel[*kernel.MapStreamIndices]] {
	return s.MapInputStreamIndicesNode
}

func (s *TranscoderWithPassthrough[C, P]) GetRecoderConfig(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	logger.Tracef(ctx, "GetRecoderConfig")
	defer func() { logger.Tracef(ctx, "/GetRecoderConfig: %v", _ret) }()
	if s == nil {
		return types.RecoderConfig{}
	}
	return xsync.DoA1R1(ctx, &s.locker, s.getRecoderConfigLocked, ctx)
}

func (s *TranscoderWithPassthrough[C, P]) getRecoderConfigLocked(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	switchValue := s.SwitchPreFilter.GetValue(ctx)
	logger.Tracef(ctx, "switchValue: %v", switchValue)
	if switchValue == 0 {
		return s.RecodingConfig
	}
	cpy := s.RecodingConfig
	cpy.Output.VideoTrackConfigs = slices.Clone(cpy.Output.VideoTrackConfigs)
	cpy.Output.VideoTrackConfigs[0].CodecName = codectypes.Name(codec.NameCopy)
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
	s.MapInputStreamIndicesNode.Processor.Kernel.Assigner.(*streamIndexAssignerInput[C, P]).reload(ctx)
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) configureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "configureRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/configureRecoder(ctx, %#+v): %v", cfg, _err) }()
	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	if len(cfg.Output.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output audio track config (received a request for %d track configs)", len(cfg.Output.AudioTrackConfigs))
	}
	if s.Recoder == nil {
		if err := s.initRecoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to initialize the recoder: %w", err)
		}
		return nil
	}
	if codec.Name(cfg.Output.AudioTrackConfigs[0].CodecName) != codec.NameCopy {
		return fmt.Errorf("we currently do not support reconfiguring audio recoding with codec '%s' (!= 'copy')", cfg.Output.AudioTrackConfigs[0].CodecName)
	}
	if codec.Name(cfg.Output.VideoTrackConfigs[0].CodecName) == codec.NameCopy {
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

	if bitRate := cfg.Output.VideoTrackConfigs[0].AverageBitRate; bitRate > 0 {
		videoQuality = quality.ConstantBitrate(bitRate)
	}

	if res := cfg.Output.VideoTrackConfigs[0].Resolution; res != (codec.Resolution{}) {
		videoResolution = &res
	}

	var err error
	s.Recoder, err = kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx,
			&codec.NaiveDecoderFactoryParams{
				HardwareDeviceType: globaltypes.HardwareDeviceType(cfg.Output.VideoTrackConfigs[0].HardwareDeviceType),
				HardwareDeviceName: globaltypes.HardwareDeviceName(cfg.Output.VideoTrackConfigs[0].HardwareDeviceName),
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
				VideoCodec:            codec.Name(cfg.Output.VideoTrackConfigs[0].CodecName),
				AudioCodec:            codec.Name(cfg.Output.AudioTrackConfigs[0].CodecName),
				HardwareDeviceType:    globaltypes.HardwareDeviceType(cfg.Output.VideoTrackConfigs[0].HardwareDeviceType),
				HardwareDeviceName:    globaltypes.HardwareDeviceName(cfg.Output.VideoTrackConfigs[0].HardwareDeviceName),
				VideoOptions:          xastiav.DictionaryItemsToAstiav(ctx, convertCustomOptions(cfg.Output.VideoTrackConfigs[0].CustomOptions)),
				AudioOptions:          xastiav.DictionaryItemsToAstiav(ctx, convertCustomOptions(cfg.Output.AudioTrackConfigs[0].CustomOptions)),
				VideoQuality:          videoQuality,
				VideoResolution:       videoResolution,
				VideoAverageFrameRate: astiav.NewRational(int(cfg.Output.VideoTrackConfigs[0].AverageFrameRate*1000), 1000),
				AudioSampleRate:       audio.SampleRate(cfg.Output.AudioTrackConfigs[0].SampleRate),
			},
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
) (_err error) {
	logger.Tracef(ctx, "reconfigureRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureRecoder(ctx, %#+v): %v", cfg, _err) }()

	encoderFactory := s.Recoder.EncoderFactory
	if codec.Name(cfg.Output.VideoTrackConfigs[0].CodecName) != encoderFactory.VideoCodec {
		return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", cfg.Output.VideoTrackConfigs[0].CodecName, encoderFactory.VideoCodec)
	}

	err := xsync.DoR1(ctx, &s.Recoder.EncoderFactory.Locker, func() error {
		videoCfg := cfg.Output.VideoTrackConfigs[0]
		if len(s.Recoder.EncoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			if s.Recoder.EncoderFactory.VideoOptions == nil {
				s.Recoder.EncoderFactory.VideoOptions = astiav.NewDictionary()
				setFinalizerFree(ctx, s.Recoder.EncoderFactory.VideoOptions)
			}

			if videoCfg.AverageBitRate == 0 {
				s.Recoder.EncoderFactory.VideoOptions.Unset("b")
				s.Recoder.EncoderFactory.VideoQuality = nil
			} else {
				s.Recoder.EncoderFactory.VideoOptions.Set("b", fmt.Sprintf("%d", videoCfg.AverageBitRate), 0)
				s.Recoder.EncoderFactory.VideoQuality = quality.ConstantBitrate(videoCfg.AverageBitRate)
			}

			if videoCfg.AverageFrameRate > 0 {
				s.Recoder.EncoderFactory.VideoAverageFrameRate.SetNum(int(videoCfg.AverageFrameRate * 1000))
				s.Recoder.EncoderFactory.VideoAverageFrameRate.SetDen(1000)
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
			res := encoder.GetResolution(ctx)
			if res == nil {
				return fmt.Errorf("unable to get the current resolution")
			}
			logger.Debugf(ctx, "current resolution: %s; requested resolution: %s", res, videoCfg.Resolution)
			if *res != videoCfg.Resolution {
				logger.Debugf(ctx, "resolution needs changing...")
				err := encoder.SetResolution(ctx, videoCfg.Resolution, nil)
				if err != nil {
					return fmt.Errorf("unable to set resolution to %v: %w", videoCfg.Resolution, err)
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

func (s *TranscoderWithPassthrough[C, P]) reconfigureRecoderCopy(
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
	s.FilterThrottle.BitrateAveragingPeriod = cfg.Output.VideoTrackConfigs[0].AveragingPeriod
	s.FilterThrottle.AverageBitRate = cfg.Output.VideoTrackConfigs[0].AverageBitRate // if AverageBitRate != 0 then here we also enable the throttler (if it was disabled)

	if switchState == 0 { // was in the recoding state
		// to avoid the recoder sending some packets from an obsolete state (when we are going to reuse it), we just reset it.
		if err := s.Recoder.ResetSoft(ctx); err != nil {
			logger.Errorf(ctx, "unable to reset the recoder: %v", err)
		}
	}

	return nil
}

func (s *TranscoderWithPassthrough[C, P]) GetAllStats(
	ctx context.Context,
) map[string]globaltypes.Statistics {
	m := map[string]globaltypes.Statistics{
		"Recoder": nodetypes.ToStatistics(s.NodeRecoder.GetCountersPtr(), s.NodeRecoder.GetProcessor().CountersPtr()),
	}
	tryGetStats := func(key string, n node.Abstract) {
		m[key] = nodetypes.ToStatistics(n.GetCountersPtr(), n.GetProcessor().CountersPtr())
	}
	tryGetStats("Input", s.MapInputStreamIndicesNode)
	for idx, output := range s.Outputs {
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

type DebugData struct {
	Transcoder any
	Original   any
}

func (s *TranscoderWithPassthrough[C, P]) Start(
	ctx context.Context,
	passthroughMode types.PassthroughMode,
	serveCfg avpipeline.ServeConfig,
) (_err error) {
	logger.Debugf(ctx, "Start(ctx, %s): %p", passthroughMode, s)
	defer logger.Debugf(ctx, "/Start(ctx, %s): %p: %v", passthroughMode, s, _err)
	if s.Recoder == nil {
		return fmt.Errorf("s.Recoder is not configured")
	}

	serveCfg.EachNode.DebugData = DebugData{
		Transcoder: s,
		Original:   serveCfg.EachNode.DebugData,
	}

	outputMain := &nodewrapper.NoServe[node.Abstract]{Node: s.Outputs[0]}
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

	if passthroughMode != types.PassthroughModeNever {
		s.MapInputStreamIndicesNode.AddPushPacketsTo(ctx,
			s.NodeRecoder,
			packetfiltercondition.Packet{
				s.SwitchPreFilter.PacketCondition(0),
				s.SwitchPostFilter.PacketCondition(0),
			},
		)
		s.MapInputStreamIndicesNode.AddPushPacketsTo(ctx,
			nodeFilterThrottle,
			packetfiltercondition.Packet{
				s.SwitchPreFilter.PacketCondition(1),
				s.SwitchPostFilter.PacketCondition(1),
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
		s.MapInputStreamIndicesNode.AddPushPacketsTo(ctx, s.NodeRecoder)
		s.MapInputStreamIndicesNode.AddPushFramesTo(ctx, s.NodeRecoder)
	}

	s.NodeStreamFixerMain = autofix.New(
		belt.WithField(ctx, "branch", "output-main"),
		s.Recoder.Encoder,
		outputAsPacketSink,
	)

	if passthroughMode != types.PassthroughModeNever {
		var passthroughOutputReference packet.Sink = s.Recoder.Decoder
		if passthroughMode == types.PassthroughModeNextOutput {
			passthroughOutputReference = asPacketSink(s.Outputs[1].GetProcessor())
		}
		s.NodeStreamFixerPassthrough = autofix.New(
			belt.WithField(ctx, "branch", "passthrough"),
			s.PacketSource,
			passthroughOutputReference,
		)
		if s.NodeStreamFixerPassthrough != nil {
			nodeFilterThrottle.AddPushPacketsTo(ctx, s.NodeStreamFixerPassthrough)
			nodeFilterThrottle.AddPushFramesTo(ctx, s.NodeStreamFixerPassthrough)
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
					rescaletsbetweenpacketsources.New(
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
		case types.PassthroughModeSameTracks:
			condRecoder = append(condRecoder, packetfiltercondition.Packet{
				s.SwitchPreFilter.PacketCondition(0),
				s.SwitchPostFilter.PacketCondition(0),
			})
			condPassthrough = append(condPassthrough, packetfiltercondition.Packet{
				s.SwitchPreFilter.PacketCondition(1),
				s.SwitchPostFilter.PacketCondition(1),
			})
			sinkMain, sinkPassthrough = outputMain, s.NodeStreamFixerMain
			if s.NodeStreamFixerMain == nil {
				sinkPassthrough = outputMain
			}
		case types.PassthroughModeSameConnection:
			nodeMapStreamIndices := node.NewFromKernel(
				ctx,
				s.MapOutputStreamIndices,
				processor.DefaultOptionsOutput()...,
			)
			nodeMapStreamIndices.AddPushPacketsTo(ctx, outputMain)
			sinkMain, sinkPassthrough = nodeMapStreamIndices, s.NodeStreamFixerMain
			if s.NodeStreamFixerMain == nil {
				sinkPassthrough = nodeMapStreamIndices
			}
		case types.PassthroughModeNextOutput:
			condRecoder = append(condRecoder, packetfiltercondition.Packet{
				s.SwitchPreFilter.PacketCondition(0),
				s.SwitchPostFilter.PacketCondition(0),
			})
			condPassthrough = append(condPassthrough, packetfiltercondition.Packet{
				s.SwitchPreFilter.PacketCondition(1),
				s.SwitchPostFilter.PacketCondition(1),
			})
			sinkMain, sinkPassthrough = outputMain, &nodewrapper.NoServe[node.Abstract]{Node: s.Outputs[1]}
		default:
			return fmt.Errorf("unknown passthrough mode: '%s'", passthroughMode)
		}
		if s.NodeStreamFixerMain != nil {
			s.NodeRecoder.AddPushPacketsTo(ctx, s.NodeStreamFixerMain, condRecoder...)
			s.NodeStreamFixerMain.AddPushPacketsTo(ctx, sinkMain)
		} else {
			s.NodeRecoder.AddPushPacketsTo(ctx, sinkMain, condRecoder...)
		}
		if s.NodeStreamFixerPassthrough != nil {
			s.NodeStreamFixerPassthrough.AddPushPacketsTo(ctx, sinkPassthrough, condPassthrough...)
		} else {
			nodeFilterThrottle.AddPushPacketsTo(ctx, sinkPassthrough, condPassthrough...)
		}
	} else {
		if s.NodeStreamFixerMain != nil {
			s.NodeRecoder.AddPushPacketsTo(ctx, s.NodeStreamFixerMain)
			s.NodeRecoder.AddPushFramesTo(ctx, s.NodeStreamFixerMain)
			s.NodeStreamFixerMain.AddPushPacketsTo(ctx, outputMain)
			s.NodeStreamFixerMain.AddPushFramesTo(ctx, outputMain)
		} else {
			s.NodeRecoder.AddPushPacketsTo(ctx, outputMain)
			s.NodeRecoder.AddPushFramesTo(ctx, outputMain)
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
				cancelFn()
				if errors.As(err.Err, &node.ErrAlreadyStarted{}) {
					logger.Errorf(ctx, "%#+v", err)
					continue
				}
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

func (s *TranscoderWithPassthrough[C, P]) Wait(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "Wait")
	defer func() { logger.Tracef(ctx, "/Wait: %v", _err) }()
	s.waitGroup.Wait()
	return nil
}
