package streammux

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/codec/resource"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	extrapacketorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition/extra"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/limitvideobitrate"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/reduceframerate"
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
)

type NodeBarrier[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
type NodeMapStreamIndexes[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.MapStreamIndices]]
type NodeRecoder[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]
type SenderKey = types.SenderKey
type SenderKeys = types.SenderKeys

type OutputID int

type OutputCustomData[C any] struct {
	*Output[C]
}

type FPSFractionGetter interface {
	GetFPSFraction(ctx context.Context) globaltypes.Rational
}

type OutputStreamsIniter interface {
	InitOutputVideoStreams(
		ctx context.Context,
		receiver node.Abstract,
		senderKey SenderKey,
	) error
}

type OutputMeasurements struct {
	RecodingStartAudioDTS atomic.Uint64
	RecodingStartVideoDTS atomic.Uint64
	RecodingEndAudioDTS   atomic.Uint64
	RecodingEndVideoDTS   atomic.Uint64
	LastSendingAudioDTS   atomic.Uint64
	LastSendingVideoDTS   atomic.Uint64
}

type Output[C any] struct {
	ID                    OutputID
	InputFrom             *NodeInput[C]
	InputFilter           *NodeBarrier[OutputCustomData[C]]
	InputThrottler        *limitvideobitrate.Filter
	InputFixer            *autofix.AutoFixerWithCustomData[OutputCustomData[C]]
	RecoderNode           *NodeRecoder[OutputCustomData[C]]
	SendingThrottler      *limitvideobitrate.Filter
	MapIndices            *NodeMapStreamIndexes[OutputCustomData[C]]
	SendingFixer          *autofix.AutoFixerWithCustomData[OutputCustomData[C]]
	SendingSyncer         *NodeBarrier[OutputCustomData[C]]
	SendingNode           SendingNode[C]
	SendingNodeProps      types.SenderNodeProps
	FPSFractionGetter     FPSFractionGetter
	InitOnce              sync.Once
	IsClosedValue         bool
	ParentResourceManager ResourceManager

	Measurements OutputMeasurements
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

type ResourceManager interface {
	resource.FreeUnneededer
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
//	  v SendingThrottler             v SendingThrottler
//	  |                              |
//	  v                              v
//	MapIndices                    MapIndices
//	  |                              |
//	  v                              v
//	SendingFixer                  SendingFixer
//	  |                              |
//	  v MonotonicPTS-· · · · · · · · v ·-MonotonicPTS
//	  |                              |
//	  v SendingSyncer- · · · · · · · v ·-SendingSyncer
//	  |                              |
//	  v                              v
//	SendingNode                   SendingNode

func newOutput[C any](
	ctx context.Context,
	outputID OutputID,
	inputNode *NodeInput[C],
	senderFactory SenderFactory[C],
	senderKey SenderKey,
	outputSwitch barrierstategetter.StateGetter,
	sendingSyncer barrierstategetter.StateGetter,
	allowCorrupt *atomic.Bool,
	monotonicPTS packetorframecondition.Condition,
	streamIndexAssigner kernel.StreamIndexAssigner,
	streamsIniter OutputStreamsIniter,
	resourceManager ResourceManager,
	fpsFractionGetter FPSFractionGetter,
) (_ret *Output[C], _err error) {
	ctx = belt.WithField(ctx, "output_id", outputID)
	ctx = xcontext.DetachDone(ctx)
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Debugf(ctx, "newOutput: %#+v", senderKey)
	defer func() {
		if _ret != nil {
			logger.Debugf(ctx, "/newOutput: %#+v: OutputID:%v", senderKey, _ret.ID)
			return
		}
		logger.Debugf(ctx, "/newOutput: %#+v: err:%v", senderKey, _err)
	}()

	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()

	if senderKey.VideoResolution == (codec.Resolution{}) && senderKey.VideoCodec != "" && senderKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output resolution is not set (codec: %s)", senderKey.VideoCodec)
	}
	if senderKey.AudioSampleRate == 0 && senderKey.AudioCodec != "" && senderKey.AudioCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output audio sample rate is not set (codec: %s)", senderKey.AudioCodec)
	}

	// construct nodes

	senderNode, sendingCfg, err := senderFactory.NewSender(ctx, senderKey)
	if err != nil {
		return nil, fmt.Errorf("unable to connect sending node: %w", err)
	}

	recoderKernel, err := kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, nil),
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:      codec.Name(senderKey.VideoCodec),
			AudioCodec:      codec.Name(senderKey.AudioCodec),
			VideoResolution: &senderKey.VideoResolution,
			AudioSampleRate: audio.SampleRate(senderKey.AudioSampleRate),
		}),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create recoder kernel: %w", err)
	}
	recoderKernel.Decoder.AllowBlankFrames = allowCorrupt

	packetSinker, ok := senderNode.GetProcessor().(processor.GetPacketSinker)
	if !ok {
		return nil, fmt.Errorf("output node %T does not implement GetPacketSinker", senderNode)
	}

	o := &Output[C]{
		ID:        outputID,
		InputFrom: inputNode,
		InputFilter: node.NewWithCustomDataFromKernel[OutputCustomData[C]](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "InputFilter"),
			outputSwitch,
		)),
		InputThrottler: limitvideobitrate.New(ctx, 0, 0),
		InputFixer: autofix.NewWithCustomData(
			belt.WithField(ctx, "output_chain_step", "InputFixer"),
			recoderKernel.Decoder,
			OutputCustomData[C]{},
		),
		RecoderNode: node.NewWithCustomDataFromKernel[OutputCustomData[C]](
			ctx,
			recoderKernel,
			processor.DefaultOptionsRecoder()...,
		),
		SendingThrottler: limitvideobitrate.New(ctx, 0, 0),
		MapIndices:       node.NewWithCustomDataFromKernel[OutputCustomData[C]](ctx, kernel.NewMapStreamIndices(ctx, streamIndexAssigner)),
		SendingFixer: autofix.NewWithCustomData(
			belt.WithField(ctx, "output_chain_step", "OutputFixer"),
			packetSinker.GetPacketSink(),
			OutputCustomData[C]{},
		),
		SendingSyncer: node.NewWithCustomDataFromKernel[OutputCustomData[C]](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "OutputSyncer"),
			sendingSyncer,
		)),
		SendingNode:           senderNode,
		FPSFractionGetter:     fpsFractionGetter,
		ParentResourceManager: resourceManager,
	}
	customData := OutputCustomData[C]{Output: o}
	o.InputFilter.CustomData = customData
	o.InputFixer.SetCustomData(customData)
	o.RecoderNode.CustomData = customData
	o.RecoderNode.SetInputPacketFilter(ctx, packetfiltercondition.Panic("the recoder is not configured, yet!"))
	codecOpts := []codec.Option{CodecOptionOutputID{OutputID: o.ID}}
	o.RecoderNode.Processor.Kernel.Decoder.DecoderFactory.ResourceManager = o.asCodecResourceManager()
	o.RecoderNode.Processor.Kernel.Decoder.DecoderFactory.Options = codecOpts
	o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.ResourceManager = o.asCodecResourceManager()
	o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.Options = codecOpts
	o.MapIndices.CustomData = customData
	o.SendingFixer.SetCustomData(customData)
	o.SendingSyncer.CustomData = customData
	o.SendingNode.SetCustomData(customData)

	if outputReuseDecoderResources {
		o.initReuseDecoderResources(ctx)
	}
	o.initFPSFractioner(ctx)

	// wiring

	var inputFixer, outputFixer node.Abstract
	inputFixer, outputFixer = o.InputFixer, o.SendingFixer
	if senderKey.VideoCodec == codectypes.Name(codec.NameCopy) {
		inputFixer, outputFixer = o.RecoderNode, o.SendingSyncer
	}

	pushToFixerConds := packetorframecondition.And{
		o.InputThrottler,
		packetorframecondition.Function(func(
			ctx context.Context,
			_ packetorframe.InputUnion,
		) bool {
			if streamsIniter == nil {
				return true
			}
			o.InitOnce.Do(func() {
				err := streamsIniter.InitOutputVideoStreams(ctx, inputFixer, senderKey)
				if err != nil {
					logger.Errorf(ctx, "unable to init output streams: %v", err)
				}
			})
			return true
		}),
	}
	o.InputFilter.AddPushPacketsTo(ctx, inputFixer, packetfiltercondition.PacketOrFrame{pushToFixerConds})
	o.InputFilter.AddPushFramesTo(ctx, inputFixer, framefiltercondition.PacketOrFrame{pushToFixerConds})
	pushToRecoderConds := packetorframecondition.Function(o.onRecoderInput)
	o.InputFixer.AddPushPacketsTo(ctx, o.RecoderNode, packetfiltercondition.PacketOrFrame{pushToRecoderConds})
	o.InputFixer.AddPushFramesTo(ctx, o.RecoderNode, framefiltercondition.PacketOrFrame{pushToRecoderConds})
	pushToMapIndicesConds := packetorframecondition.Function(o.onRecoderOutput)
	o.RecoderNode.AddPushPacketsTo(ctx, o.MapIndices, packetfiltercondition.PacketOrFrame{pushToMapIndicesConds})
	o.RecoderNode.AddPushFramesTo(ctx, o.MapIndices, framefiltercondition.PacketOrFrame{pushToMapIndicesConds})
	o.MapIndices.AddPushPacketsTo(ctx, outputFixer)
	o.MapIndices.AddPushFramesTo(ctx, outputFixer)
	pushToSenderConds := packetorframecondition.Function(o.onSenderInput)
	o.SendingFixer.AddPushPacketsTo(ctx, o.SendingSyncer, packetfiltercondition.PacketOrFrame{pushToSenderConds})
	o.SendingFixer.AddPushFramesTo(ctx, o.SendingSyncer, framefiltercondition.PacketOrFrame{pushToSenderConds})
	maxQueueSizeGetter := mathcondition.GetterFunction[uint64](func(context.Context) uint64 {
		return sendingCfg.OutputThrottlerMaxQueueSizeBytes
	})
	if monotonicPTS == nil {
		monotonicPTS = packetorframecondition.Static(true)
	}
	pushToSendingNodeConds := packetorframecondition.And{
		monotonicPTS,
		o.SendingThrottler,
		packetorframecondition.Or{
			packetorframecondition.Function(func(ctx context.Context, input packetorframe.InputUnion) bool {
				return sendingCfg.OutputThrottlerMaxQueueSizeBytes <= 0
			}),
			packetorframecondition.Not{packetorframecondition.MediaType(astiav.MediaTypeVideo)},
			packetorframecondition.IsKeyFrame(true),
			extrapacketorframecondition.PushQueueSize(
				o.SendingNode,
				mathcondition.LessOrEqualVariable(maxQueueSizeGetter),
			),
		},
	}
	o.SendingSyncer.AddPushPacketsTo(ctx, o.SendingNode, packetfiltercondition.PacketOrFrame{pushToSendingNodeConds})
	o.SendingSyncer.AddPushFramesTo(ctx, o.SendingNode, framefiltercondition.PacketOrFrame{pushToSendingNodeConds})

	// logging

	logger.Tracef(ctx, "o.InputFilter.Processor.Kernel.Handler.Condition: %p", o.InputFilter.Processor.Kernel.Handler.Condition)
	logger.Tracef(ctx, "o.OutputSyncer.Processor.Kernel.Handler.Condition: %p", o.SendingSyncer.Processor.Kernel.Handler.Condition)

	return o, nil
}

func logIfError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	logger.Errorf(ctx, "got an error: %v", err)
}

func (o *Output[C]) onRecoderInput(
	_ context.Context,
	i packetorframe.InputUnion,
) bool {
	dtsRaw := i.GetDTS()
	if dtsRaw == astiav.NoPtsValue || dtsRaw == 0 {
		dtsRaw = i.GetPTS()
	}
	dts := avconv.Duration(dtsRaw, i.GetTimeBase())
	switch i.GetMediaType() {
	case astiav.MediaTypeVideo:
		o.Measurements.RecodingStartVideoDTS.Store(uint64(dts))
	case astiav.MediaTypeAudio:
		o.Measurements.RecodingStartAudioDTS.Store(uint64(dts))
	}
	return true
}

func (o *Output[C]) onRecoderOutput(
	_ context.Context,
	i packetorframe.InputUnion,
) bool {
	dtsRaw := i.GetDTS()
	if dtsRaw == astiav.NoPtsValue || dtsRaw == 0 {
		dtsRaw = i.GetPTS()
	}
	dts := avconv.Duration(dtsRaw, i.GetTimeBase())
	switch i.GetMediaType() {
	case astiav.MediaTypeVideo:
		o.Measurements.RecodingEndVideoDTS.Store(uint64(dts))
	case astiav.MediaTypeAudio:
		o.Measurements.RecodingEndAudioDTS.Store(uint64(dts))
	}
	return true
}

func (o *Output[C]) onSenderInput(
	_ context.Context,
	i packetorframe.InputUnion,
) bool {
	dtsRaw := i.GetDTS()
	if dtsRaw == astiav.NoPtsValue || dtsRaw == 0 {
		dtsRaw = i.GetPTS()
	}
	dts := avconv.Duration(dtsRaw, i.GetTimeBase())
	switch i.GetMediaType() {
	case astiav.MediaTypeVideo:
		o.Measurements.LastSendingVideoDTS.Store(uint64(dts))
	case astiav.MediaTypeAudio:
		o.Measurements.LastSendingAudioDTS.Store(uint64(dts))
	}
	return true
}

func (o *Output[C]) String() string {
	if o == nil {
		return "streammux.Output(nil)"
	}
	return fmt.Sprintf("StreamMux.Outputs[%d]", o.ID)
}

func (o *Output[C]) FirstNodeAfterFilter() node.Abstract {
	if o.InputFixer != nil {
		return o.InputFixer
	}
	return o.RecoderNode
}

func (o *Output[C]) initFPSFractioner(ctx context.Context) {
	logger.Tracef(ctx, "initFPSFractioner()")
	defer func() { logger.Tracef(ctx, "/initFPSFractioner()") }()

	if o.FPSFractionGetter == nil {
		return
	}

	o.RecoderNode.Processor.Kernel.Filter = framecondition.Or{
		framecondition.Not{framecondition.MediaType(astiav.MediaTypeVideo)},
		framecondition.PacketOrFrame{
			reduceframerate.New(mathcondition.GetterFunction[globaltypes.Rational](
				o.FPSFractionGetter.GetFPSFraction,
			)),
		},
	}
}

func (o *Output[C]) initReuseDecoderResources(
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

		if input.CustomOptions == nil {
			input.CustomOptions = astiav.NewDictionary()
			setFinalizerFree(ctx, input.CustomOptions)
		}

		if input.HardwareDeviceType == globaltypes.HardwareDeviceTypeMediaCodec {
			logIfError(ctx, input.CustomOptions.Set("pixel_format", "mediacodec", 0))
			logIfError(ctx, input.CustomOptions.Set("create_window", "1", 0))
		}
	}
}

func (o *Output[C]) Close(ctx context.Context) (_err error) {
	return o.close(ctx, true)
}

func (o *Output[C]) CloseNoDrain(ctx context.Context) (_err error) {
	return o.close(ctx, false)
}

func (o *Output[C]) close(ctx context.Context, shouldDrain bool) (_err error) {
	logger.Tracef(ctx, "Output.close(%v)", shouldDrain)
	defer func() { logger.Tracef(ctx, "/Output.close(%v): %v", shouldDrain, _err) }()
	o.IsClosedValue = true

	var errs []error

	node.AppendInputPacketFilter(ctx, o.FirstNodeAfterFilter(), packetfiltercondition.Static(false))
	node.AppendInputFrameFilter(ctx, o.FirstNodeAfterFilter(), framefiltercondition.Static(false))
	if shouldDrain {
		if err := o.DrainAfterFilter(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to flush %d: %w", o.ID, err))
		}
	}
	if err := o.InputFilter.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input filter for output %d: %w", o.ID, err))
	}
	if err := o.InputFixer.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close input fixer for output %d: %w", o.ID, err))
	}
	if err := o.RecoderNode.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close recoder node for output %d: %w", o.ID, err))
	}
	if err := o.MapIndices.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close map indices for output %d: %w", o.ID, err))
	}
	if err := o.SendingFixer.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output fixer for output %d: %w", o.ID, err))
	}
	if err := o.SendingSyncer.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output sync filter for output %d: %w", o.ID, err))
	}
	if err := o.SendingNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close output node for output %d: %w", o.ID, err))
	}
	return errors.Join(errs...)
}

func (o *Output[C]) Input() node.Abstract {
	return o.InputFilter
}

func (o *Output[C]) Output() node.Abstract {
	return o.SendingNode
}

func (o *Output[C]) Flush(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output[%d].Flush()", o.ID)
	defer func() { logger.Tracef(ctx, "/Output[%d].Flush(): %v", o.ID, _err) }()
	for _, n := range o.Nodes() {
		logger.Tracef(ctx, "flushing %s:%p", n, n)
		err := n.Flush(ctx)
		if err != nil {
			return fmt.Errorf("unable to flush %s: %w", n, err)
		}
	}
	return nil
}

func (o *Output[C]) Drain(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output[%d].Drain()", o.ID)
	defer func() { logger.Tracef(ctx, "/Output[%d].Drain(): %v", o.ID, _err) }()
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

func (o *Output[C]) DrainAfterFilter(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Output[%d].DrainAfterFilter()", o.ID)
	defer func() { logger.Tracef(ctx, "/Output[%d].DrainAfterFilter(): %v", o.ID, _err) }()
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

func (o *Output[C]) Deinit(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "Output[%d].Deinit()", o.ID)
	defer func() { logger.Tracef(ctx, "/Output[%d].Deinit(): %v", o.ID, _err) }()
	var errs []error

	if err := o.RecoderNode.Processor.Kernel.ResetHard(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset recoder node for output %d: %w", o.ID, err))
	}

	if err := o.SendingNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to deinit recoder node for output %d: %w", o.ID, err))
	}

	return errors.Join(errs...)
}

func PartialSenderKeyFromRecoderConfig(
	ctx context.Context,
	c *types.RecoderConfig,
) SenderKey {
	if c == nil {
		return SenderKey{}
	}

	var audioCodec codec.Name
	var audioSampleRate audio.SampleRate
	if len(c.Output.AudioTrackConfigs) > 0 {
		audioCodec = codec.Name(c.Output.AudioTrackConfigs[0].CodecName).Canonicalize(ctx, true)
		audioSampleRate = c.Output.AudioTrackConfigs[0].SampleRate
	}
	var videoCodec codec.Name
	var resolution codec.Resolution
	if len(c.Output.VideoTrackConfigs) > 0 {
		videoCodec = codec.Name(c.Output.VideoTrackConfigs[0].CodecName).Canonicalize(ctx, true)
		resolution = c.Output.VideoTrackConfigs[0].Resolution
	}
	return SenderKey{
		AudioCodec:      codectypes.Name(audioCodec),
		AudioSampleRate: audioSampleRate,
		VideoCodec:      codectypes.Name(videoCodec),
		VideoResolution: resolution,
	}
}

func (o *Output[C]) reconfigureRecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureRecoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureRecoder(ctx, %#+v): %v", cfg, _err) }()

	isCopyEncoder, err := o.reconfigureEncoder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to reconfigure the encoder: %w", err)
	}
	if cfg.Output.VideoTrackConfigs[0].CodecName == codectypes.Name(codec.NameCopy) && !isCopyEncoder {
		logger.Errorf(ctx, "the encoder is not a copy encoder despite it should be")
		isCopyEncoder = true
	}
	if !isCopyEncoder {
		if err := o.reconfigureDecoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to reconfigure the decoder: %w", err)
		}
	}

	o.RecoderNode.SetInputPacketFilter(ctx, nil)
	return nil
}

func (o *Output[C]) reconfigureDecoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureDecoder: %#+v", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureDecoder: %#+v: %v", cfg, _err) }()
	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0] // TODO: it should use cfg.Input!

	var videoOptions globaltypes.DictionaryItems
	for _, opt := range convertCustomOptions(videoCfg.CustomOptions) {
		switch opt.Key {
		case "create_window":
			videoOptions = append(videoOptions, opt)
		case "pixel_format":
			videoOptions = append(videoOptions, opt)
		}
	}

	decoder := o.RecoderNode.Processor.Kernel.Decoder
	decoderFactory := decoder.DecoderFactory

	err := xsync.DoR1(ctx, &decoder.Locker, func() error {
		if len(decoder.Decoders) == 0 {
			logger.Debugf(ctx, "the decoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")
			decoderFactory.HardwareDeviceType = videoCfg.GetDecoderHardwareDeviceType()
			decoderFactory.HardwareDeviceName = codec.HardwareDeviceName(videoCfg.GetDecoderHardwareDeviceName())
			decoderFactory.VideoOptions = xastiav.DictionaryItemsToAstiav(ctx, videoOptions)
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

func (o *Output[C]) reconfigureEncoder(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_isCopyEncoder bool, _err error) {
	logger.Tracef(ctx, "reconfigureEncoder: %#+v", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureEncoder: %#+v: %v", cfg, _err) }()

	var videoCfg types.OutputVideoTrackConfig
	if len(cfg.Output.VideoTrackConfigs) > 1 {
		return false, fmt.Errorf("currently we support only one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	if len(cfg.Output.VideoTrackConfigs) > 0 {
		videoCfg = cfg.Output.VideoTrackConfigs[0]
	}

	var audioCfg types.OutputAudioTrackConfig
	if len(cfg.Output.AudioTrackConfigs) > 1 {
		return false, fmt.Errorf("currently we support only one output audio track config (received a request for %d track configs)", len(cfg.Output.AudioTrackConfigs))
	}
	if len(cfg.Output.AudioTrackConfigs) > 0 {
		audioCfg = cfg.Output.AudioTrackConfigs[0]
	}

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
			encoderFactory.AudioSampleRate = audioCfg.SampleRate
			return nil
		}

		if codec.Name(videoCfg.CodecName) != encoderFactory.VideoCodec {
			return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%s' != '%s'", videoCfg.CodecName, encoderFactory.VideoCodec)
		}

		logger.Debugf(ctx, "the encoder is already initialized, so modifying it if needed")
		encoder := encoderFactory.VideoEncoders[0]
		_isCopyEncoder = codec.IsEncoderCopy(encoder)

		if videoCfg.HardwareDeviceType != types.HardwareDeviceType(encoderFactory.HardwareDeviceType) {
			return fmt.Errorf("unable to change the hardware device type on the fly, yet: '%s' != '%s'", videoCfg.HardwareDeviceType, encoderFactory.HardwareDeviceType)
		}

		if videoCfg.HardwareDeviceName != types.HardwareDeviceName(encoderFactory.HardwareDeviceName) {
			return fmt.Errorf("unable to change the hardware device name on the fly, yet: '%s' != '%s'", videoCfg.HardwareDeviceName, encoderFactory.HardwareDeviceName)
		}

		if audioCfg.SampleRate != encoderFactory.AudioSampleRate {
			return fmt.Errorf("unable to change the audio sample rate on the fly, yet: '%d' != '%d'", audioCfg.SampleRate, encoderFactory.AudioSampleRate)
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

		if !_isCopyEncoder {
			res := encoder.GetResolution(ctx)
			if res == nil {
				return fmt.Errorf("unable to get the current encoding resolution from encoder %s", encoder)
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
		return _isCopyEncoder, err
	}

	return _isCopyEncoder, nil
}

func canonicalizeCodecName(ctx context.Context, name codec.Name) codectypes.Name {
	return codectypes.Name(name.Canonicalize(ctx, true))
}

func (o *Output[C]) GetKey() SenderKey {
	var videoResolution codec.Resolution
	if o.RecoderNode.Processor.Kernel.EncoderFactory.VideoResolution != nil {
		videoResolution = *o.RecoderNode.Processor.Kernel.EncoderFactory.VideoResolution
	}
	ctx := context.Background()
	return SenderKey{
		AudioCodec:      canonicalizeCodecName(ctx, o.RecoderNode.Processor.Kernel.EncoderFactory.AudioCodec),
		AudioSampleRate: o.RecoderNode.Processor.Kernel.EncoderFactory.AudioSampleRate,
		VideoCodec:      canonicalizeCodecName(ctx, o.RecoderNode.Processor.Kernel.EncoderFactory.VideoCodec),
		VideoResolution: videoResolution,
	}
}

func (o *Output[C]) Nodes() []node.Abstract {
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
		o.SendingFixer,
		o.SendingSyncer,
		o.SendingNode,
	)
	return result
}

func (o *Output[C]) NodesAfterFilter() []node.Abstract {
	return o.Nodes()[1:]
}

func (o *Output[C]) IsClosed() bool {
	return o.IsClosedValue
}

func (o *Output[C]) SetForceNextFrameKey(
	ctx context.Context,
	forceNextFrameKey bool,
) (_err error) {
	logger.Tracef(ctx, "Output[%d].SetForceNextFrameKey(%v)", o.ID, forceNextFrameKey)
	defer func() { logger.Tracef(ctx, "/Output[%d].SetForceNextFrameKey(%v): %v", o.ID, forceNextFrameKey, _err) }()

	o.RecoderNode.Processor.Kernel.Encoder.SetForceNextKeyFrame(ctx, forceNextFrameKey)
	return nil
}
