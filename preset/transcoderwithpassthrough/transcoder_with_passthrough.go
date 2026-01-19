// transcoder_with_passthrough.go implements a preset for transcoding with a passthrough fallback.

// Package transcoderwithpassthrough provides a preset for transcoding with a passthrough fallback.
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
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packet/filter/rescaletsbetweenpacketsources"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/limitvideobitrate"
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
	FilterThrottle            *limitvideobitrate.Filter
	SwitchPreFilter           *packetcondition.Switch
	SwitchPostFilter          *packetcondition.Switch
	Transcoder                *kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	MapInputStreamIndicesNode *node.Node[*processor.FromKernel[*kernel.MapStreamIndices]]
	MapOutputStreamIndices    *kernel.MapStreamIndices
	NodeTranscoder            *node.Node[*processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]

	NodeStreamFixerMain        *autofix.AutoFixer
	NodeStreamFixerPassthrough *autofix.AutoFixer

	TranscodingConfig types.TranscoderConfig

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
		FilterThrottle:   limitvideobitrate.New(ctx, 0, 0),
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
		processor.OptionQueueSizeInput(6000),
		processor.OptionQueueSizeOutput(10),
		processor.OptionQueueSizeError(2),
	)
	s.inputStreamMapIndicesAsPacketSource = asPacketSource(s.MapInputStreamIndicesNode.Processor)
	s.MapOutputStreamIndices = kernel.NewMapStreamIndices(ctx, newStreamIndexAssignerOutput(s))
	return s, nil
}

func (s *TranscoderWithPassthrough[C, P]) Input() *node.Node[*processor.FromKernel[*kernel.MapStreamIndices]] {
	return s.MapInputStreamIndicesNode
}

func (s *TranscoderWithPassthrough[C, P]) GetTranscoderConfig(
	ctx context.Context,
) (_ret types.TranscoderConfig) {
	logger.Tracef(ctx, "GetTranscoderConfig")
	defer func() { logger.Tracef(ctx, "/GetTranscoderConfig: %v", _ret) }()
	if s == nil {
		return types.TranscoderConfig{}
	}
	return xsync.DoA1R1(ctx, &s.locker, s.getTranscoderConfigLocked, ctx)
}

func (s *TranscoderWithPassthrough[C, P]) getTranscoderConfigLocked(
	ctx context.Context,
) (_ret types.TranscoderConfig) {
	switchValue := s.SwitchPreFilter.GetValue(ctx)
	logger.Tracef(ctx, "switchValue: %v", switchValue)
	if switchValue == 0 {
		return s.TranscodingConfig
	}
	cpy := s.TranscodingConfig
	cpy.Output.VideoTrackConfigs = slices.Clone(cpy.Output.VideoTrackConfigs)
	cpy.Output.VideoTrackConfigs[0].CodecName = codectypes.Name(codec.NameCopy)
	return cpy
}

func (s *TranscoderWithPassthrough[C, P]) SetTranscoderConfig(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "SetTranscoderConfig(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/SetTranscoderConfig(ctx, %#+v): %v", cfg, _err) }()
	return xsync.DoA2R1(ctx, &s.locker, s.setTranscoderConfigLocked, ctx, cfg)
}

func (s *TranscoderWithPassthrough[C, P]) setTranscoderConfigLocked(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	err := s.configureTranscoder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to configure the transcoder: %w", err)
	}
	s.TranscodingConfig = cfg
	s.MapInputStreamIndicesNode.Processor.Kernel.Assigner.(*streamIndexAssignerInput[C, P]).reload(ctx)
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) configureTranscoder(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "configureTranscoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/configureTranscoder(ctx, %#+v): %v", cfg, _err) }()
	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	if len(cfg.Output.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output audio track config (received a request for %d track configs)", len(cfg.Output.AudioTrackConfigs))
	}
	if s.Transcoder == nil {
		if err := s.initTranscoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to initialize the transcoder: %w", err)
		}
		return nil
	}
	if codec.Name(cfg.Output.AudioTrackConfigs[0].CodecName) != codec.NameCopy {
		return fmt.Errorf("we currently do not support reconfiguring audio transcoding with codec '%s' (!= 'copy')", cfg.Output.AudioTrackConfigs[0].CodecName)
	}
	if codec.Name(cfg.Output.VideoTrackConfigs[0].CodecName) == codec.NameCopy {
		if err := s.reconfigureTranscoderCopy(ctx, cfg); err != nil {
			return fmt.Errorf("unable to reconfigure to copying: %w", err)
		}
		return nil
	}
	if err := s.reconfigureTranscoder(ctx, cfg); err != nil {
		return fmt.Errorf("unable to reconfigure the transcoder: %w", err)
	}
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) initTranscoder(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "initTranscoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/initTranscoder(ctx, %#+v): %v", cfg, _err) }()

	if s.Transcoder != nil {
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
	s.Transcoder, err = kernel.NewTranscoder(
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
		return fmt.Errorf("unable to initialize a transcoder: %w", err)
	}
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) reconfigureTranscoder(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureTranscoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureTranscoder(ctx, %#+v): %v", cfg, _err) }()

	encoderFactory := s.Transcoder.EncoderFactory
	if codec.Name(cfg.Output.VideoTrackConfigs[0].CodecName) != encoderFactory.VideoCodec {
		return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", cfg.Output.VideoTrackConfigs[0].CodecName, encoderFactory.VideoCodec)
	}

	err := xsync.DoR1(ctx, &s.Transcoder.EncoderFactory.Locker, func() error {
		videoCfg := cfg.Output.VideoTrackConfigs[0]
		if len(s.Transcoder.EncoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			if s.Transcoder.EncoderFactory.VideoOptions == nil {
				s.Transcoder.EncoderFactory.VideoOptions = astiav.NewDictionary()
				setFinalizerFree(ctx, s.Transcoder.EncoderFactory.VideoOptions)
			}

			if videoCfg.AverageBitRate == 0 {
				s.Transcoder.EncoderFactory.VideoOptions.Unset("b")
				s.Transcoder.EncoderFactory.VideoQuality = nil
			} else {
				s.Transcoder.EncoderFactory.VideoOptions.Set("b", fmt.Sprintf("%d", videoCfg.AverageBitRate), 0)
				s.Transcoder.EncoderFactory.VideoQuality = quality.ConstantBitrate(videoCfg.AverageBitRate)
			}

			if videoCfg.AverageFrameRate > 0 {
				s.Transcoder.EncoderFactory.VideoAverageFrameRate.SetNum(int(videoCfg.AverageFrameRate * 1000))
				s.Transcoder.EncoderFactory.VideoAverageFrameRate.SetDen(1000)
			}
			return nil
		}

		logger.Debugf(ctx, "the encoder is already initialized, so modifying it if needed")
		encoder := s.Transcoder.EncoderFactory.VideoEncoders[0]

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
		return fmt.Errorf("unable to switch the pre-filter to transcoding: %w", err)
	}

	return nil
}

func (s *TranscoderWithPassthrough[C, P]) reconfigureTranscoderCopy(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureTranscoderCopy(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureTranscoderCopy(ctx, %#+v): %v", cfg, _err) }()

	switchState := s.SwitchPreFilter.CurrentValue.Load()

	err := s.SwitchPreFilter.SetValue(ctx, 1)
	if err != nil {
		return fmt.Errorf("unable to switch the pre-filter to passthrough: %w", err)
	}
	s.FilterThrottle.BitrateAveragingPeriod = cfg.Output.VideoTrackConfigs[0].AveragingPeriod
	s.FilterThrottle.AverageBitRate = cfg.Output.VideoTrackConfigs[0].AverageBitRate // if AverageBitRate != 0 then here we also enable the throttler (if it was disabled)

	if switchState == 0 { // was in the transcoding state
		// to avoid the transcoder sending some packets from an obsolete state (when we are going to reuse it), we just reset it.
		if err := s.Transcoder.ResetSoft(ctx); err != nil {
			logger.Errorf(ctx, "unable to reset the transcoder: %v", err)
		}
	}

	return nil
}

func (s *TranscoderWithPassthrough[C, P]) GetAllStats(
	ctx context.Context,
) map[string]globaltypes.Statistics {
	m := map[string]globaltypes.Statistics{
		"Transcoder": nodetypes.ToStatistics(s.NodeTranscoder.GetCountersPtr(), s.NodeTranscoder.GetProcessor().CountersPtr()),
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
	if s.Transcoder == nil {
		return fmt.Errorf("s.Transcoder is not configured")
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

	s.NodeTranscoder = node.NewFromKernel(
		ctx,
		s.Transcoder,
		processor.DefaultOptionsTranscoder()...,
	)
	nodeFilterThrottle := node.NewFromKernel(
		ctx,
		kernel.NewFilter(s.FilterThrottle),
		processor.DefaultOptionsOutput()...,
	)

	if passthroughMode != types.PassthroughModeNever {
		s.MapInputStreamIndicesNode.AddPushTo(ctx,
			s.NodeTranscoder,
			packetorframefiltercondition.PacketFilter{
				Condition: packetfiltercondition.Packet{
					s.SwitchPreFilter.PacketCondition(0),
					s.SwitchPostFilter.PacketCondition(0),
				},
			},
		)
		s.MapInputStreamIndicesNode.AddPushTo(ctx,
			nodeFilterThrottle,
			packetorframefiltercondition.PacketFilter{
				Condition: packetfiltercondition.Packet{
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
		s.MapInputStreamIndicesNode.AddPushTo(ctx, s.NodeTranscoder)
	}

	s.NodeStreamFixerMain = autofix.New(
		belt.WithField(ctx, "branch", "output-main"),
		outputAsPacketSink,
	)

	if passthroughMode != types.PassthroughModeNever {
		var passthroughOutputReference packet.Sink = s.Transcoder.Decoder
		if passthroughMode == types.PassthroughModeNextOutput {
			passthroughOutputReference = asPacketSink(s.Outputs[1].GetProcessor())
		}
		s.NodeStreamFixerPassthrough = autofix.New(
			belt.WithField(ctx, "branch", "passthrough"),
			passthroughOutputReference,
		)
		if s.NodeStreamFixerPassthrough != nil {
			nodeFilterThrottle.AddPushTo(ctx, s.NodeStreamFixerPassthrough)
		}

		if startWithPassthrough {
			s.SwitchPreFilter.CurrentValue.Store(1)
			s.SwitchPostFilter.CurrentValue.Store(1)
			s.SwitchPreFilter.NextValue.Store(1)
			s.SwitchPostFilter.NextValue.Store(1)
		}

		if passthroughRescaleTS {
			if !startWithPassthrough || notifyAboutPacketSources {
				nodeFilterThrottle.InputFilter = packetorframefiltercondition.PacketFilter{
					Condition: packetfiltercondition.Packet{
						rescaletsbetweenpacketsources.New(
							s.PacketSource,
							s.NodeTranscoder.Processor.Kernel.Encoder,
						),
					},
				}
			} else {
				logger.Warnf(ctx, "unable to configure rescale_ts because startWithPassthrough && !notifyAboutPacketSources")
			}
		}

		var condTranscoder, condPassthrough packetorframefiltercondition.And
		var sinkMain, sinkPassthrough node.Abstract
		switch passthroughMode {
		case types.PassthroughModeSameTracks:
			condTranscoder = append(condTranscoder, packetorframefiltercondition.PacketFilter{
				Condition: packetfiltercondition.Packet{
					s.SwitchPreFilter.PacketCondition(0),
					s.SwitchPostFilter.PacketCondition(0),
				},
			})
			condPassthrough = append(condPassthrough, packetorframefiltercondition.PacketFilter{
				Condition: packetfiltercondition.Packet{
					s.SwitchPreFilter.PacketCondition(1),
					s.SwitchPostFilter.PacketCondition(1),
				},
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
			nodeMapStreamIndices.AddPushTo(ctx, outputMain)
			sinkMain, sinkPassthrough = nodeMapStreamIndices, s.NodeStreamFixerMain
			if s.NodeStreamFixerMain == nil {
				sinkPassthrough = nodeMapStreamIndices
			}
		case types.PassthroughModeNextOutput:
			condTranscoder = append(condTranscoder, packetorframefiltercondition.PacketFilter{
				Condition: packetfiltercondition.Packet{
					s.SwitchPreFilter.PacketCondition(0),
					s.SwitchPostFilter.PacketCondition(0),
				},
			})
			condPassthrough = append(condPassthrough, packetorframefiltercondition.PacketFilter{
				Condition: packetfiltercondition.Packet{
					s.SwitchPreFilter.PacketCondition(1),
					s.SwitchPostFilter.PacketCondition(1),
				},
			})
			sinkMain, sinkPassthrough = outputMain, &nodewrapper.NoServe[node.Abstract]{Node: s.Outputs[1]}
		default:
			return fmt.Errorf("unknown passthrough mode: '%s'", passthroughMode)
		}
		if s.NodeStreamFixerMain != nil {
			s.NodeTranscoder.AddPushTo(ctx, s.NodeStreamFixerMain, condTranscoder...)
			s.NodeStreamFixerMain.AddPushTo(ctx, sinkMain)
		} else {
			s.NodeTranscoder.AddPushTo(ctx, sinkMain, condTranscoder...)
		}
		if s.NodeStreamFixerPassthrough != nil {
			s.NodeStreamFixerPassthrough.AddPushTo(ctx, sinkPassthrough, condPassthrough...)
		} else {
			nodeFilterThrottle.AddPushTo(ctx, sinkPassthrough, condPassthrough...)
		}
	} else {
		if s.NodeStreamFixerMain != nil {
			s.NodeTranscoder.AddPushTo(ctx, s.NodeStreamFixerMain)
			s.NodeStreamFixerMain.AddPushTo(ctx, outputMain)
		} else {
			s.NodeTranscoder.AddPushTo(ctx, outputMain)
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
