package transcoderwithpassthrough

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
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
	rescaleTS                  = true
	notifyAboutPacketSources   = true
	startWithPassthrough       = false
	autoInsertBitstreamFilters = true
	passthroughSupport         = true
)

type TranscoderWithPassthrough[C any, P processor.Abstract] struct {
	Input             *node.NodeWithCustomData[C, P]
	Outputs           []node.Abstract
	FilterThrottle    *packetcondition.VideoAverageBitrateLower
	PassthroughSwitch *packetcondition.Switch
	PostSwitchFilter  *packetcondition.Switch
	BothPipesSwitch   *packetcondition.Static
	Recoder           *kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	MapStreamIndices  *kernel.MapStreamIndices
	NodeRecoder       *node.Node[*processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]

	RecodingConfig types.RecoderConfig

	inputAsPacketSource packet.Source

	locker    sync.Mutex
	waitGroup sync.WaitGroup
}

/*
//           +--> THROTTLE ->---+
// INPUT ->--+                  +--> (MAP INDICES) --> OUTPUT
//           +--> RECODER -->---+
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
	s.MapStreamIndices = kernel.NewMapStreamIndices(ctx, newStreamIndexAssigner(s))
	s.inputAsPacketSource = asPacketSource(s.Input.GetProcessor())
	if s.inputAsPacketSource == nil {
		return nil, fmt.Errorf("the input node processor is expected to be a packet source, but is not")
	}

	return s, nil
}

func (s *TranscoderWithPassthrough[C, P]) GetRecoderConfig(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	logger.Tracef(ctx, "GetRecoderConfig")
	defer func() { logger.Tracef(ctx, "/GetRecoderConfig: %v", _ret) }()
	s.locker.Lock()
	defer s.locker.Unlock()
	switchValue := s.PassthroughSwitch.GetValue(ctx)
	logger.Tracef(ctx, "switchValue: %v", switchValue)
	if switchValue == 0 {
		return s.RecodingConfig
	}
	cpy := s.RecodingConfig
	cpy.Video.CodecName = codec.CodecNameCopy
	return cpy
}

func (s *TranscoderWithPassthrough[C, P]) SetRecoderConfig(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "SetRecoderConfig(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/SetRecoderConfig(ctx, %#+v): %v", cfg, _err) }()
	s.locker.Lock()
	defer s.locker.Unlock()
	err := s.configureRecoder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to reconfigure the recoder: %w", err)
	}
	s.RecodingConfig = cfg
	return nil
}

func (s *TranscoderWithPassthrough[C, P]) configureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) error {
	if s.Recoder == nil {
		if err := s.initRecoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to initialize the recoder: %w", err)
		}
		return nil
	}
	if cfg.Audio.CodecName != "copy" {
		return fmt.Errorf("we currently do not support audio recoding: '%s' != 'copy'", cfg.Audio.CodecName)
	}
	if cfg.Video.CodecName == "copy" {
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
			avptypes.HardwareDeviceType(cfg.Video.HardwareDeviceType),
			avptypes.HardwareDeviceName(cfg.Video.HardwareDeviceName),
			nil,
			nil,
		),
		codec.NewNaiveEncoderFactory(ctx,
			cfg.Video.CodecName,
			"copy",
			avptypes.HardwareDeviceType(cfg.Video.HardwareDeviceType),
			avptypes.HardwareDeviceName(cfg.Video.HardwareDeviceName),
			convertCustomOptions(cfg.Video.CustomOptions),
			convertCustomOptions(cfg.Audio.CustomOptions),
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
	if cfg.Video.CodecName != encoderFactory.VideoCodec {
		return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", cfg.Video.CodecName, encoderFactory.VideoCodec)
	}

	err := xsync.DoR1(ctx, &s.Recoder.EncoderFactory.Locker, func() error {
		if len(s.Recoder.EncoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			if s.Recoder.EncoderFactory.VideoOptions == nil {
				s.Recoder.EncoderFactory.VideoOptions = astiav.NewDictionary()
				setFinalizerFree(ctx, s.Recoder.EncoderFactory.VideoOptions)
			}

			if cfg.Video.AverageBitRate == 0 {
				s.Recoder.EncoderFactory.VideoOptions.Unset("b")
			} else {
				s.Recoder.EncoderFactory.VideoOptions.Set("b", fmt.Sprintf("%d", cfg.Video.AverageBitRate), 0)
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
			if q == quality.ConstantBitrate(cfg.Video.AverageBitRate) {
				needsChangingBitrate = false
			}
		}

		if needsChangingBitrate && cfg.Video.AverageBitRate > 0 {
			err := encoder.SetQuality(ctx, quality.ConstantBitrate(cfg.Video.AverageBitRate), nil)
			if err != nil {
				return fmt.Errorf("unable to set bitrate to %v: %w", cfg.Video.AverageBitRate, err)
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
	s.FilterThrottle.BitrateAveragingPeriod = cfg.Video.AveragingPeriod
	s.FilterThrottle.AverageBitRate = cfg.Video.AverageBitRate // if AverageBitRate != 0 then here we also enable the throttler (if it was disabled)
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

func tryNewBSFForMPEG2(
	ctx context.Context,
	videoCodecID astiav.CodecID,
	_ astiav.CodecID,
) *node.Node[*processor.FromKernel[*kernel.BitstreamFilter]] {
	recoderVideoBSFName := bitstreamfilter.NameMP4ToMP2(videoCodecID)
	if recoderVideoBSFName == bitstreamfilter.NameNull {
		return nil
	}

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, map[packetcondition.Condition]bitstreamfilter.Name{
		packetcondition.MediaType(astiav.MediaTypeVideo): recoderVideoBSFName,
	})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter '%s': %w", recoderVideoBSFName, err)
		return nil
	}

	return node.NewFromKernel(
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}

func tryNewBSFForMPEG4(
	ctx context.Context,
	_ astiav.CodecID,
	audioCodecID astiav.CodecID,
) *node.Node[*processor.FromKernel[*kernel.BitstreamFilter]] {
	recoderAudioBSFName := bitstreamfilter.NameMP2ToMP4(audioCodecID)
	if recoderAudioBSFName == bitstreamfilter.NameNull {
		return nil
	}

	bitstreamFilter, err := kernel.NewBitstreamFilter(ctx, map[packetcondition.Condition]bitstreamfilter.Name{
		packetcondition.MediaType(astiav.MediaTypeAudio): recoderAudioBSFName,
	})
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the bitstream filter '%s': %w", recoderAudioBSFName, err)
		return nil
	}

	return node.NewFromKernel(
		ctx,
		bitstreamFilter,
		processor.DefaultOptionsOutput()...,
	)
}

func getCodecNamesFromStreams(streams []*astiav.Stream) (astiav.CodecID, astiav.CodecID) {
	var videoCodecID, audioCodecID astiav.CodecID
	for _, stream := range streams {
		switch stream.CodecParameters().MediaType() {
		case astiav.MediaTypeVideo:
			videoCodecID = stream.CodecParameters().CodecID()
		case astiav.MediaTypeAudio:
			audioCodecID = stream.CodecParameters().CodecID()
		}
	}
	return videoCodecID, audioCodecID
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

	ctx, cancelFn := context.WithCancel(ctx)

	// == configure ==

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

	var outputFormatName string
	outputAsPacketSink.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		outputFormatName = fmtCtx.OutputFormat().Name()
	})
	logger.Infof(ctx, "output format: '%s'", outputFormatName)

	var recoderOutput node.Abstract = s.NodeRecoder
	var nodeBSFPassthrough *node.Node[*processor.FromKernel[*kernel.BitstreamFilter]]
	{
		var inputVideoCodecID, inputAudioCodecID astiav.CodecID
		s.inputAsPacketSource.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			inputVideoCodecID, inputAudioCodecID = getCodecNamesFromStreams(
				fmtCtx.Streams(),
			)
		})
		recodedVideoCodecID := s.Recoder.EncoderFactory.VideoCodecID()
		if recodedVideoCodecID == 0 { // vcodec: 'copy'
			recodedVideoCodecID = inputVideoCodecID
		}
		recodedAudioCodecID := s.Recoder.EncoderFactory.AudioCodecID()
		if recodedAudioCodecID == 0 { // acodec: 'copy'
			recodedAudioCodecID = inputAudioCodecID
		}

		var nodeBSFRecoder *node.Node[*processor.FromKernel[*kernel.BitstreamFilter]]
		switch outputFormatName {
		case "mpegts", "rtsp":
			nodeBSFRecoder = tryNewBSFForMPEG2(ctx, recodedVideoCodecID, recodedAudioCodecID)
			nodeBSFPassthrough = tryNewBSFForMPEG2(ctx, inputVideoCodecID, inputAudioCodecID)
		case "flv":
			nodeBSFRecoder = tryNewBSFForMPEG4(ctx, recodedVideoCodecID, recodedAudioCodecID)
			nodeBSFPassthrough = tryNewBSFForMPEG4(ctx, inputVideoCodecID, inputAudioCodecID)
		}
		if autoInsertBitstreamFilters && nodeBSFRecoder != nil {
			logger.Debugf(ctx, "inserting %s to the recoder's output", nodeBSFRecoder.Processor.Kernel)
			recoderOutput.AddPushPacketsTo(nodeBSFRecoder)
			recoderOutput = nodeBSFRecoder
		}
	}

	var secondNodesInChain []node.Abstract
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
						if keyFrameCount%10 == 1 || true {
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
						if audioFrameCount%10 == 1 || true {
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

		s.Input.AddPushPacketsTo(
			s.NodeRecoder,
			packetcondition.Or{
				packetcondition.And{
					s.PassthroughSwitch.Condition(0),
					s.PostSwitchFilter.Condition(0),
				},
				bothPipesSwitch,
			},
		)
		secondNodesInChain = append(secondNodesInChain, s.NodeRecoder)
		s.Input.AddPushPacketsTo(
			nodeFilterThrottle,
			packetcondition.Or{
				packetcondition.And{
					s.PassthroughSwitch.Condition(1),
					s.PostSwitchFilter.Condition(1),
				},
				bothPipesSwitch,
			},
		)
		secondNodesInChain = append(secondNodesInChain, nodeFilterThrottle)

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
				s.MapStreamIndices,
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
						s.NodeRecoder.Processor.Kernel.Decoder,
					),
					filter.NewRescaleTSBetweenKernels(
						s.NodeRecoder.Processor.Kernel.Encoder,
						outputAsPacketSink,
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
		s.Input.AddPushPacketsTo(s.NodeRecoder)
		secondNodesInChain = append(secondNodesInChain, s.NodeRecoder)
		recoderOutput.AddPushPacketsTo(output)
	}

	removeSubscriptionToInput := func(ctx context.Context) error {
		var errs []error
		for _, dstNode := range secondNodesInChain {
			if err := node.RemovePushPacketsTo(ctx, s.Input, dstNode); err != nil {
				errs = append(errs, fmt.Errorf("unable to remove packet pushing from Input to %s: %w", dstNode, err))
			}
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

	errCh := make(chan node.Error, 10)
	s.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer s.waitGroup.Done()
		defer cancelFn()
		defer logger.Debugf(ctx, "finished the error listening loop")
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

		dontServeNodes := nodecondition.In{s.Input}
		dontServeNodes = append(dontServeNodes, nodecondition.In(s.Outputs)...)
		avpipeline.Serve(ctx, avpipeline.ServeConfig{
			NodeFilter: nodecondition.Not{dontServeNodes},
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
