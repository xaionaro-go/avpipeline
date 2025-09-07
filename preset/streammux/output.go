package streammux

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/resourcegetter"
	resourcegettercondition "github.com/xaionaro-go/avpipeline/codec/resourcegetter/condition"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	extrapacketcondition "github.com/xaionaro-go/avpipeline/packet/condition/extra"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/quality"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	xastiav "github.com/xaionaro-go/avpipeline/types/astiav"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
)

const (
	outputReuseDecoderResources = false
	outputDebug                 = true
)

type NodeBarrier[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
type NodeMapStreamIndexes[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.MapStreamIndices]]
type NodeRecoder[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]
type OutputKey = types.OutputKey
type OutputKeys = types.OutputKeys

type FPSFractionGetter interface {
	GetFPSFraction(ctx context.Context) (num, den uint32)
}

type OutputStreamsIniter interface {
	InitOutputStreams(ctx context.Context, receiver node.Abstract, outputKey OutputKey) error
}

type OutputCustomData struct {
	*Output
}

type Output struct {
	ID                int
	InputFilter       *NodeBarrier[OutputCustomData]
	InputThrottler    *packetcondition.VideoAverageBitrateLower
	InputFixer        *autofix.AutoFixerWithCustomData[OutputCustomData]
	RecoderNode       *NodeRecoder[OutputCustomData]
	OutputThrottler   *packetcondition.VideoAverageBitrateLower
	MapIndices        *NodeMapStreamIndexes[OutputCustomData]
	OutputFixer       *autofix.AutoFixerWithCustomData[OutputCustomData]
	OutputSyncer      *NodeBarrier[OutputCustomData]
	OutputNode        node.Abstract
	OutputNodeConfig  OutputConfig
	FPSFractionGetter FPSFractionGetter
	InitOnce          sync.Once
}

type OutputConfig struct {
	OutputThrottlerMaxQueueSizeBytes uint64
}

type initOutputConfig struct{}

type InitOutputOption interface {
	apply(*initOutputConfig)
}

type InitOutputOptions []InitOutputOption

func (opts InitOutputOptions) apply(cfg *initOutputConfig) {
	for _, opt := range opts {
		opt.apply(cfg)
	}
}

func (opts InitOutputOptions) config() initOutputConfig {
	cfg := initOutputConfig{}
	opts.apply(&cfg)
	return cfg
}

// An example with two Outputs:
//
//                 Input
//                   |
//                   v
//	  +------<-------+------->-------+
//	  |                              |
//	  v                              v
//	InputFilter(OutputSwitch)    InputFilter(OutputSwitch)
//	  |                              |
//	  v InputThrottler               v InputThrottler
//	  |                              |
//	  v                              v
//	InputFixer                   InputFixer
//	  |                              |
//	  v                              v
//	dyn(RecoderNode)             dyn(RecoderNode)
//	  |                              |
//	  v OutputThrottler              v OutputThrottler
//	  |                              |
//	  v                              v
//	OutputMapIndices             OutputMapIndices
//	  |                              |
//	  v                              v
//	OutputFixer                  OutputFixer
//	  |                              |
//	  v OutputSyncer-· · · · · · · · v ·-OutputSyncer
//	  |                              |
//	  v                              v
//	OutputNode                   OutputNode

func newOutput[C any](
	ctx context.Context,
	outputID int,
	inputNode *NodeInput[C],
	outputFactory OutputFactory,
	outputKey OutputKey,
	outputSwitch barrierstategetter.StateGetter,
	outputSyncer barrierstategetter.StateGetter,
	streamIndexAssigner kernel.StreamIndexAssigner,
	streamsIniter OutputStreamsIniter,
	fpsFractionGetter FPSFractionGetter,
) (_ret *Output, _err error) {
	ctx = belt.WithField(ctx, "output_id", outputID)
	ctx = xcontext.DetachDone(ctx)
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Debugf(ctx, "newOutput: %#+v", outputKey)
	defer func() { logger.Debugf(ctx, "/newOutput: %#+v %v", outputKey, _err) }()

	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()

	if outputKey.Resolution == (codec.Resolution{}) && outputKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output resolution is not set")
	}

	// construct nodes

	outputNode, outputConfig, err := outputFactory.NewOutput(ctx, outputKey)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output node: %w", err)
	}
	if outputNode == nil {
		return nil, fmt.Errorf("output node is nil")
	}

	recoderKernel, err := kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, nil),
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:      codec.Name(outputKey.VideoCodec),
			AudioCodec:      codec.Name(outputKey.AudioCodec),
			VideoResolution: &outputKey.Resolution,
		}),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create recoder kernel: %w", err)
	}

	packetSinker, ok := outputNode.GetProcessor().(processor.GetPacketSinker)
	if !ok {
		return nil, fmt.Errorf("output node %T does not implement GetPacketSinker", outputNode)
	}

	o := &Output{
		ID: outputID,
		InputFilter: node.NewWithCustomDataFromKernel[OutputCustomData](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "InputFilter"),
			outputSwitch,
		)),
		InputThrottler: packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		InputFixer: autofix.NewWithCustomData[OutputCustomData](
			belt.WithField(ctx, "output_chain_step", "InputFixer"),
			inputNode.Processor.GetPacketSource(),
			recoderKernel.Decoder,
			OutputCustomData{},
		),
		RecoderNode: node.NewWithCustomDataFromKernel[OutputCustomData](
			ctx,
			recoderKernel,
			processor.DefaultOptionsRecoder()...,
		),
		OutputThrottler: packetcondition.NewVideoAverageBitrateLower(ctx, 0, 0),
		MapIndices:      node.NewWithCustomDataFromKernel[OutputCustomData](ctx, kernel.NewMapStreamIndices(ctx, streamIndexAssigner)),
		OutputFixer: autofix.NewWithCustomData[OutputCustomData](
			belt.WithField(ctx, "output_chain_step", "OutputFixer"),
			recoderKernel.Encoder,
			packetSinker.GetPacketSink(),
			OutputCustomData{},
		),
		OutputSyncer: node.NewWithCustomDataFromKernel[OutputCustomData](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "OutputSyncer"),
			outputSyncer,
		)),
		OutputNode:        outputNode,
		OutputNodeConfig:  outputConfig,
		FPSFractionGetter: fpsFractionGetter,
	}
	o.InputFilter.CustomData = OutputCustomData{Output: o}
	o.InputFixer.SetCustomData(OutputCustomData{Output: o})
	o.RecoderNode.CustomData = OutputCustomData{Output: o}
	o.MapIndices.CustomData = OutputCustomData{Output: o}
	o.OutputFixer.SetCustomData(OutputCustomData{Output: o})
	o.OutputSyncer.CustomData = OutputCustomData{Output: o}

	if outputReuseDecoderResources {
		o.initReuseDecoderResources(ctx)
	}
	o.initFPSFractioner(ctx)

	// wiring

	var inputFixer, outputFixer node.Abstract
	inputFixer, outputFixer = o.InputFixer, o.OutputFixer
	if outputKey.VideoCodec == codectypes.Name(codec.NameCopy) {
		inputFixer, outputFixer = o.RecoderNode, o.OutputSyncer
	}

	o.InputFilter.AddPushPacketsTo(
		inputFixer,
		packetfiltercondition.Packet{Condition: o.InputThrottler},
		packetfiltercondition.Function(func(
			ctx context.Context,
			_ packetfiltercondition.Input,
		) bool {
			if streamsIniter == nil {
				return true
			}
			o.InitOnce.Do(func() {
				err := streamsIniter.InitOutputStreams(ctx, inputFixer, outputKey)
				if err != nil {
					logger.Errorf(ctx, "unable to init output streams: %v", err)
				}
			})
			return true
		}),
	)
	o.InputFixer.AddPushPacketsTo(o.RecoderNode)
	o.RecoderNode.AddPushPacketsTo(
		o.MapIndices,
	)
	o.MapIndices.AddPushPacketsTo(outputFixer)
	o.OutputFixer.AddPushPacketsTo(o.OutputSyncer)
	maxQueueSizeGetter := mathcondition.GetterFunction[uint64](func() uint64 {
		return o.OutputNodeConfig.OutputThrottlerMaxQueueSizeBytes
	})
	o.OutputSyncer.AddPushPacketsTo(o.OutputNode,
		packetfiltercondition.Packet{Condition: packetcondition.And{
			o.OutputThrottler,
			packetcondition.Or{
				packetcondition.Function(func(context.Context, packet.Input) bool {
					return o.OutputNodeConfig.OutputThrottlerMaxQueueSizeBytes <= 0
				}),
				extrapacketcondition.PushQueueSize(
					o.OutputNode,
					mathcondition.LessOrEqualVariable(maxQueueSizeGetter),
				),
			},
		}},
	)

	// logging

	logger.Tracef(ctx, "o.InputFilter.Processor.Kernel.Handler.Condition: %p", o.InputFilter.Processor.Kernel.Handler.Condition)
	logger.Tracef(ctx, "o.OutputSyncer.Processor.Kernel.Handler.Condition: %p", o.OutputSyncer.Processor.Kernel.Handler.Condition)

	return o, nil
}

func logIfError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	logger.Errorf(ctx, "got an error: %v", err)
}

func (o *Output) String() string {
	if o == nil {
		return "streammux.Output(nil)"
	}
	return fmt.Sprintf("StreamMux.Outputs[%d]", o.ID)
}

func (o *Output) FirstNodeAfterFilter() node.Abstract {
	if o.InputFixer != nil {
		return o.InputFixer
	}
	return o.RecoderNode
}

func (o *Output) initFPSFractioner(ctx context.Context) {
	logger.Tracef(ctx, "initFPSFractioner()")
	defer func() { logger.Tracef(ctx, "/initFPSFractioner()") }()

	var frameCount atomic.Uint32
	o.RecoderNode.Processor.Kernel.Filter = framecondition.Function(func(
		ctx context.Context,
		i frame.Input,
	) (_ret bool) {
		if i.GetMediaType() != astiav.MediaTypeVideo {
			return true
		}

		num, den := o.FPSFractionGetter.GetFPSFraction(ctx)
		if outputDebug {
			logger.Tracef(ctx, "frame %p from stream %d: pts:%d, dur:%d: %v/%v", i.Frame, i.StreamIndex, i.Frame.Pts(), i.Frame.Duration(), num, den)
			defer func() {
				logger.Tracef(ctx, "/frame %p from stream %d: pts:%d, dur:%d: %v/%v: %v", i.Frame, i.StreamIndex, i.Frame.Pts(), i.Frame.Duration(), num, den, _ret)
			}()
		}
		if den == 0 {
			return true
		}
		i.Frame.SetDuration(i.Frame.Duration() * int64(den) / int64(num))
		frameID := frameCount.Add(1)
		shouldPass := frameID%den < num
		if outputDebug {
			logger.Tracef(ctx, "shouldPass: %t: frameID=%d, num=%d, den=%d", shouldPass, frameID, num, den)
		}
		return shouldPass
	})
}

func (o *Output) initReuseDecoderResources(
	ctx context.Context,
) {
	logger.Tracef(ctx, "initReuseDecoderResources()")
	defer func() { logger.Tracef(ctx, "/initReuseDecoderResources()") }()

	encRes := *o.RecoderNode.Processor.Kernel.EncoderFactory.VideoResolution

	// use reusable (by encoder) pixel_format without offloading to CPU
	o.RecoderNode.Processor.Kernel.DecoderFactory.PreInitFunc = func(
		ctx context.Context,
		stream *astiav.Stream,
		input *codec.DecoderInput,
	) {
		logger.Tracef(ctx, "PreInitFunc(ctx, stream=%p, input=%v)", stream, input)
		defer func() { logger.Tracef(ctx, "/PreInitFunc(ctx, stream=%p, input=%v)", stream, input) }()

		if stream.CodecParameters().MediaType() != astiav.MediaTypeVideo {
			logger.Debugf(ctx, "not a video stream")
			return
		}

		if stream.CodecParameters().Width() != int(encRes.Width) {
			logger.Debugf(ctx, "unable to reuse the decoder resources: width mismatch: %d != %d", stream.CodecParameters().Width(), encRes.Width)
			return
		}

		if stream.CodecParameters().Height() != int(encRes.Height) {
			logger.Debugf(ctx, "unable to reuse the decoder resources: height mismatch: %d != %d", stream.CodecParameters().Height(), encRes.Height)
			return
		}

		if input.Options == nil {
			input.Options = astiav.NewDictionary()
			setFinalizerFree(ctx, input.Options)
		}

		if input.HardwareDeviceType == globaltypes.HardwareDeviceTypeMediaCodec {
			logIfError(ctx, input.Options.Set("pixel_format", "mediacodec", 0))
			logIfError(ctx, input.Options.Set("create_window", "1", 0))
		}
	}

	// allow sharing the resources between decoder and encoder
	o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.ResourcesGetter = resourcegetter.NewConditional(
		o.RecoderNode.Processor.Kernel.Decoder.DecoderFactory,
		resourcegettercondition.Function(func(
			ctx context.Context,
			f resourcegetter.Input,
		) (_ret bool) {
			logger.Tracef(ctx, "checking if we can reuse resources")
			defer func() { logger.Tracef(ctx, "/checking if we can reuse resources: %v", _ret) }()

			getDecoderer, ok := codec.EncoderFactoryOptionLatest[codec.EncoderFactoryOptionGetDecoderer](f.Options)
			if !ok {
				logger.Debugf(ctx, "unable to find FrameSource in the EncoderFactory options: %#+v", f.Options)
				return false
			}

			// we can reuse the resources only if no scaling is required
			if f.Params.Width() != int(encRes.Width) {
				return false
			}
			if f.Params.Height() != int(encRes.Height) {
				return false
			}

			// we can reuse the resources only if pixel format is the same
			decoder := getDecoderer.GetDecoderer.GetDecoder()
			if f.Params.PixelFormat() != astiav.PixelFormatNone {
				if f.Params.PixelFormat() != decoder.CodecContext().PixelFormat() {
					return false
				}
			}
			return true
		}),
	)
}

func (o *Output) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output.Close()")
	defer func() { logger.Tracef(ctx, "/Output.Close(): %v", _err) }()
	var errs []error

	o.FirstNodeAfterFilter().SetInputPacketFilter(packetfiltercondition.Static(false))
	o.FirstNodeAfterFilter().SetInputFrameFilter(framefiltercondition.Static(false))
	if err := o.FlushAfterFilter(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to flush %d: %w", o.ID, err))
	}
	if err := o.InputFilter.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input filter for output %d: %w", o.ID, err))
	}
	if err := o.InputFixer.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input fixer for output %d: %w", o.ID, err))
	}
	if err := o.RecoderNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close recoder node for output %d: %w", o.ID, err))
	}
	if err := o.MapIndices.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close map indices for output %d: %w", o.ID, err))
	}
	if err := o.OutputFixer.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output fixer for output %d: %w", o.ID, err))
	}
	if err := o.OutputSyncer.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output sync filter for output %d: %w", o.ID, err))
	}
	if err := o.OutputNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output node for output %d: %w", o.ID, err))
	}
	return errors.Join(errs...)
}

func (o *Output) Input() node.Abstract {
	return o.InputFilter
}

func (o *Output) Output() node.Abstract {
	return o.OutputNode
}

func (o *Output) Flush(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output[%d].Flush()", o.ID)
	defer func() { logger.Tracef(ctx, "/Output[%d].Flush(): %v", o.ID, _err) }()
	for _, n := range o.Nodes() {
		logger.Tracef(ctx, "draining the inputs of %s:%p", n, n)
		err := processor.DrainInput(ctx, n.GetProcessor())
		if err != nil {
			return fmt.Errorf("unable to drain input of %s: %w", n, err)
		}

		logger.Tracef(ctx, "flushing %s:%p", n, n)
		err = n.Flush(ctx)
		if err != nil {
			return fmt.Errorf("unable to flush %s: %w", n, err)
		}

		logger.Tracef(ctx, "waiting for drain of %s:%p", n, n)
		err = node.WaitForDrain(ctx, n)
		if err != nil {
			return fmt.Errorf("unable to wait for drain the output %d: %w", o.ID, err)
		}
	}
	return nil
}

func (o *Output) FlushAfterFilter(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output[%d].FlushAfterFilter()", o.ID)
	defer func() { logger.Tracef(ctx, "/Output[%d].FlushAfterFilter(): %v", o.ID, _err) }()
	for _, n := range o.NodesAfterFilter() {
		logger.Tracef(ctx, "draining the inputs of %s:%p", n, n)
		err := processor.DrainInput(ctx, n.GetProcessor())
		if err != nil {
			return fmt.Errorf("unable to drain input of %s: %w", n, err)
		}

		logger.Tracef(ctx, "flushing %s:%p", n, n)
		err = n.Flush(ctx)
		if err != nil {
			return fmt.Errorf("unable to flush %s: %w", n, err)
		}

		logger.Tracef(ctx, "waiting for drain of %s:%p", n, n)
		err = node.WaitForDrain(ctx, n)
		if err != nil {
			return fmt.Errorf("unable to wait for drain the output %d: %w", o.ID, err)
		}
	}
	return nil
}

func OutputKeyFromRecoderConfig(
	ctx context.Context,
	c *types.RecoderConfig,
) OutputKey {
	if c == nil {
		return OutputKey{}
	}

	var audioCodec codec.Name
	if len(c.Output.AudioTrackConfigs) > 0 {
		audioCodec = codec.Name(c.Output.AudioTrackConfigs[0].CodecName).Canonicalize(ctx, true)
	}
	var videoCodec codec.Name
	var resolution codec.Resolution
	if len(c.Output.VideoTrackConfigs) > 0 {
		videoCodec = codec.Name(c.Output.VideoTrackConfigs[0].CodecName).Canonicalize(ctx, true)
		resolution = c.Output.VideoTrackConfigs[0].Resolution
	}
	return OutputKey{
		AudioCodec: codectypes.Name(audioCodec),
		VideoCodec: codectypes.Name(videoCodec),
		Resolution: resolution,
	}
}

func (o *Output) reconfigureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureRecoder(ctx, %#+v): %v", cfg, _err) }()

	if err := o.reconfigureDecoder(ctx, cfg); err != nil {
		return fmt.Errorf("unable to reconfigure the decoder: %w", err)
	}
	if err := o.reconfigureEncoder(ctx, cfg); err != nil {
		return fmt.Errorf("unable to reconfigure the encoder: %w", err)
	}
	return nil
}

func (o *Output) reconfigureDecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureDecoder: %#+v", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureDecoder: %#+v: %v", cfg, _err) }()
	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0]

	decoder := o.RecoderNode.Processor.Kernel.Decoder
	decoderFactory := decoder.DecoderFactory

	err := xsync.DoR1(ctx, &decoder.Locker, func() error {
		if len(decoder.Decoders) == 0 {
			logger.Debugf(ctx, "the decoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")
			decoderFactory.HardwareDeviceType = videoCfg.GetDecoderHardwareDeviceType()
			decoderFactory.HardwareDeviceName = codec.HardwareDeviceName(videoCfg.GetDecoderHardwareDeviceName())
			return nil
		}
		if videoCfg.GetDecoderHardwareDeviceType() != types.HardwareDeviceType(decoderFactory.HardwareDeviceType) {
			return fmt.Errorf("unable to change the decoding hardware device type on the fly, yet: '%s' != '%s'", videoCfg.GetDecoderHardwareDeviceType(), decoderFactory.HardwareDeviceType)
		}
		if videoCfg.GetDecoderHardwareDeviceName() != types.HardwareDeviceName(decoderFactory.HardwareDeviceName) {
			return fmt.Errorf("unable to change the decoding hardware device name on the fly, yet: '%s' != '%s'", videoCfg.GetDecoderHardwareDeviceName(), decoderFactory.HardwareDeviceName)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (o *Output) reconfigureEncoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureEncoder: %#+v", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureEncoder: %#+v: %v", cfg, _err) }()
	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0]

	if len(cfg.Output.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output audio track config (received a request for %d track configs)", len(cfg.Output.AudioTrackConfigs))
	}
	audioCfg := cfg.Output.AudioTrackConfigs[0]

	encoderFactory := o.RecoderNode.Processor.Kernel.EncoderFactory

	var videoOptions globaltypes.DictionaryItems
	videoOptions = append(videoOptions, globaltypes.DictionaryItems{
		{Key: "forced-idr", Value: "1"},    // to avoid corruptions on switching the outputs
		{Key: "intra-refresh", Value: "0"}, // to avoid corruptions on switching the outputs
	}...)
	videoOptions = append(videoOptions, convertCustomOptions(videoCfg.CustomOptions)...)

	err := xsync.DoR1(ctx, &encoderFactory.Locker, func() error {
		if len(encoderFactory.VideoEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			encoderFactory.VideoCodec = codec.Name(videoCfg.CodecName)
			encoderFactory.AudioCodec = codec.Name(audioCfg.CodecName)
			encoderFactory.AudioOptions = xastiav.DictionaryItemsToAstiav(ctx, convertCustomOptions(audioCfg.CustomOptions))
			encoderFactory.VideoOptions = xastiav.DictionaryItemsToAstiav(ctx, videoOptions)
			encoderFactory.HardwareDeviceName = codec.HardwareDeviceName(videoCfg.HardwareDeviceName)
			encoderFactory.HardwareDeviceType = types.HardwareDeviceType(videoCfg.HardwareDeviceType)
			if videoCfg.AverageBitRate != 0 {
				encoderFactory.VideoQuality = quality.ConstantBitrate(videoCfg.AverageBitRate)
			}
			if videoCfg.Resolution != (codectypes.Resolution{}) {
				encoderFactory.VideoResolution = &videoCfg.Resolution
			}
			fps := globaltypes.RationalFromApproxFloat64(videoCfg.AverageFrameRate)
			encoderFactory.VideoAverageFrameRate = astiav.NewRational(fps.Num, fps.Den)
			encoderFactory.VideoAverageFrameRate.SetDen(1000)
			return nil
		}

		if codec.Name(videoCfg.CodecName) != encoderFactory.VideoCodec {
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

		{
			res := encoder.GetResolution(ctx)
			if res == nil {
				return fmt.Errorf("unable to get the current encoding resolution")
			}
			logger.Debugf(ctx,
				"current resolution: %#+v; requested resolution: %#+v",
				*res, videoCfg.Resolution,
			)
			if videoCfg.Resolution != (codectypes.Resolution{}) && *res != videoCfg.Resolution {
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

	return nil
}

func (o *Output) GetKey() OutputKey {
	var res codec.Resolution
	if o.RecoderNode.Processor.Kernel.EncoderFactory.VideoResolution != nil {
		res = *o.RecoderNode.Processor.Kernel.EncoderFactory.VideoResolution
	}
	return OutputKey{
		AudioCodec: codectypes.Name(o.RecoderNode.Processor.Kernel.EncoderFactory.AudioCodec),
		VideoCodec: codectypes.Name(o.RecoderNode.Processor.Kernel.EncoderFactory.VideoCodec),
		Resolution: res,
	}
}

func (o *Output) Nodes() []node.Abstract {
	// the order must be the same as the packets/frames flow,
	// otherwise flushing/draining will deadlock
	result := []node.Abstract{
		o.InputFilter,
	}
	if o.InputFixer != nil {
		result = append(result, o.InputFixer)
	}
	result = append(result,
		o.RecoderNode,
		o.MapIndices,
		o.OutputFixer,
		o.OutputSyncer,
		o.OutputNode,
	)
	return result
}

func (o *Output) NodesAfterFilter() []node.Abstract {
	return o.Nodes()[1:]
}
