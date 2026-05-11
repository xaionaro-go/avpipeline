// output.go implements the output handling for the stream muxer.

package streammux

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	frameconditionextra "github.com/xaionaro-go/avpipeline/frame/condition/extra"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
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
	// outputReuseDecoderResources gates the decoder→encoder hardware-context
	// share path that turns transcoding into surface passthrough. With this
	// enabled the streammux Output's internal Transcoder decoder produces
	// pix_fmt=mediacodec frames (via PreInitFunc-injected
	// "pixel_format=mediacodec, create_window=1") and the encoder reuses the
	// same HWDeviceContext (carrying the persistent input ANativeWindow), so
	// the codec context opens with avctx->pix_fmt=AV_PIX_FMT_MEDIACODEC and
	// the surface passthrough path activates inside mediacodecenc.c (line
	// 468). Without it the decoder downloads to nv12 and the encoder
	// re-uploads on the CPU side ('forcing nv12 pixel format' warning,
	// ~150% CPU baseline on a Pixel 8a).
	outputReuseDecoderResources = true
)

type (
	NodeBarrier[C any]          = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Barrier]]
	NodeMapStreamIndexes[C any] = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.MapStreamIndices]]
	NodeTranscoder[C any]       = node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]]
	SenderKey                   = types.SenderKey
	SenderKeys                  = types.SenderKeys
)

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
	TranscodingStartAudioDTS atomic.Uint64
	TranscodingStartVideoDTS atomic.Uint64
	TranscodingEndAudioDTS   atomic.Uint64
	TranscodingEndVideoDTS   atomic.Uint64
	LastSendingAudioDTS      atomic.Uint64
	LastSendingVideoDTS      atomic.Uint64
}

type Output[C any] struct {
	ID                    OutputID
	InputFrom             *NodeInput[C]
	InputFilter           *NodeBarrier[OutputCustomData[C]]
	InputThrottler        *limitvideobitrate.Filter
	InputFixer            *autofix.AutoFixerWithCustomData[OutputCustomData[C]]
	TranscoderNode        *NodeTranscoder[OutputCustomData[C]]
	SendingThrottler      *limitvideobitrate.Filter
	MapIndices            *NodeMapStreamIndexes[OutputCustomData[C]]
	SendingFixer          *autofix.AutoFixerWithCustomData[OutputCustomData[C]]
	SendingSyncer         *NodeBarrier[OutputCustomData[C]]
	SendingNode           SendingNode[C]
	SendingNodeProps      types.SenderNodeProps
	FPSFractionGetter     FPSFractionGetter
	InitOnce              sync.Once
	IsClosedValue         atomic.Bool
	CancelFn              context.CancelFunc
	ParentResourceManager ResourceManager

	Measurements OutputMeasurements

	// RawFrameSource records whether the upstream pipeline supplies decoded
	// frames directly (e.g. android_camera + android_microphone) instead of
	// packets that need a downstream decoder. When the encoder is a
	// MediaCodec encoder, no upstream decoder means there is no shared
	// ANativeWindow Surface and the get_format -> AV_PIX_FMT_MEDIACODEC
	// branch in mediacodecenc.c silently consumes frames (it expects
	// Surface buffers attached as frame->data[3]). reconfigureEncoder
	// reads this flag to inject pix_fmt=nv12 into the encoder's open-time
	// options, which forces the SW-upload encode path.
	//
	// OneWayBool pins the sticky-true contract at the type level: once
	// the upstream supplies raw frames, the encoder's pix_fmt selection
	// is locked in for the rest of the Output's life — see StreamMux.
	RawFrameSource globaltypes.OneWayBool

	// storageKey is the SenderKey under which this Output was indexed in
	// StreamMux.OutputsMap by getOrCreateOutputLocked. It is the
	// authoritative key for OutputsMap CompareAndDelete / LoadAndDelete
	// callers: GetKey() derives the key live from the EncoderFactory
	// state, but reconfigureEncoder writes BOTH EncoderFactory.VideoCodec
	// and EncoderFactory.AudioCodec onto the same factory — so a
	// video-only Output that was stored under
	// SenderKey{VideoCodec: av1, ...} starts returning a compound
	// SenderKey{VideoCodec: av1, AudioCodec: aac, ...} from GetKey() once
	// reconfigured. Map lookups keyed on GetKey() then silently miss the
	// stored entry. Using storageKey for the OutputsMap detach side keeps
	// GetKey()'s autobitrate semantics intact while eliminating the
	// silent miss in evictDeadOutput / removeOutputByIDLocked.
	storageKey SenderKey

	// senderURLAtCreation records the URL the SenderFactory generated
	// for this Output at construction time, when the factory implements
	// the optional SenderURLPreviewer capability. Empty when the factory
	// does not expose URL preview (drift detection is disabled in that
	// case and the regular Reuse path is taken).
	//
	// The Reuse path in StreamMux.getOrCreateOutputLocked compares this
	// against the URL the factory would now generate for the same
	// senderKey: a mismatch means SetOutputURL changed the template
	// since this Output was constructed, and the existing sender is
	// publishing to the stale URL. Reuse detects the drift, tears the
	// old Output down, and falls through to the Create path so the new
	// sender picks up the new URL via NewSender.
	senderURLAtCreation string
}

// StorageKey returns the SenderKey under which this Output was indexed
// in StreamMux.OutputsMap. Use this (not GetKey()) for OutputsMap detach
// callsites that must round-trip through the same key the entry was
// stored with.
func (o *Output[C]) StorageKey() SenderKey {
	return o.storageKey
}

type initOutputConfig struct {
	RawFrameSource bool
}

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

// OptionRawFrameSource declares that the upstream pipeline supplies decoded
// frames directly (no upstream decoder). Set true for camera/microphone
// inputs to force MediaCodec encoders onto the SW-upload path and avoid the
// silent-consume trap caused by the absent ANativeWindow Surface. See
// Output.RawFrameSource for details.
type OptionRawFrameSource bool

var _ InitOutputOption = OptionRawFrameSource(false)

func (o OptionRawFrameSource) apply(cfg *initOutputConfig) {
	cfg.RawFrameSource = bool(o)
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
//	dyn(TranscoderNode)             dyn(TranscoderNode)
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
	cfg initOutputConfig,
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

	// Record the URL the factory would generate for this senderKey, for
	// SetOutputURL drift detection on the Reuse path (Task #174). Empty
	// string when the factory does not expose URL preview — drift
	// detection becomes a no-op in that case.
	var senderURLAtCreation string
	if previewer, ok := senderFactory.(SenderURLPreviewer); ok {
		previewedURL, urlErr := previewer.URLForKey(ctx, senderKey)
		if urlErr == nil {
			senderURLAtCreation = previewedURL
		}
	}

	transcoderKernel, err := kernel.NewTranscoder(
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
		return nil, fmt.Errorf("failed to create transcoder kernel: %w", err)
	}
	transcoderKernel.Decoder.AllowBlankFrames = allowCorrupt

	packetSinker, ok := senderNode.GetProcessor().(processor.GetPacketSinker)
	if !ok {
		return nil, fmt.Errorf("output node %T does not implement GetPacketSinker", senderNode)
	}

	o := &Output[C]{
		ID:                  outputID,
		storageKey:          senderKey,
		senderURLAtCreation: senderURLAtCreation,
		InputFrom:           inputNode,
		InputFilter: node.NewWithCustomDataFromKernel[OutputCustomData[C]](ctx, kernel.NewBarrier(
			belt.WithField(ctx, "output_chain_step", "InputFilter"),
			outputSwitch,
		)),
		InputThrottler: limitvideobitrate.New(ctx, 0, 0),
		InputFixer: autofix.NewWithCustomData(
			belt.WithField(ctx, "output_chain_step", "InputFixer"),
			transcoderKernel.Decoder,
			OutputCustomData[C]{},
		),
		TranscoderNode: node.NewWithCustomDataFromKernel[OutputCustomData[C]](
			ctx,
			transcoderKernel,
			processor.DefaultOptionsTranscoder()...,
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
		CancelFn:              cancelFn,
		ParentResourceManager: resourceManager,
	}
	if cfg.RawFrameSource {
		o.RawFrameSource.Set()
	}
	customData := OutputCustomData[C]{Output: o}
	o.InputFilter.CustomData = customData
	o.InputFixer.SetCustomData(customData)
	o.TranscoderNode.CustomData = customData
	// The original filter was packetorframefiltercondition.Panic which
	// fired and crashed the whole daemon if any packet arrived at this
	// output before the transcoder finished configuring. This is reachable
	// during cascade init — a brief window where shared-takeover route
	// publishers route a packet through MapStreamIndices to an output
	// whose InputFilter has not yet been replaced by configurePackets.
	//
	// The architectural fix is to defer InputFilter installation until
	// after the transcoder is fully wired (or to gate route forwarding on
	// the configurePackets ack). Pending that, we soft-drop with a warning
	// so the pre-configuration race doesn't take the whole daemon down.
	// The drop is safe: pre-configuration packets cannot meaningfully be
	// processed by the not-yet-configured transcoder anyway, and
	// configurePackets replaces the filter once the transcoder is ready,
	// so subsequent packets pass through normally.
	o.TranscoderNode.SetInputFilter(ctx, packetorframefiltercondition.Function(
		func(ctx context.Context, _ packetorframefiltercondition.Input) bool {
			logger.Warnf(ctx, "the transcoder is not configured, yet! dropping packet on output %p (pre-configuration race; see streammux/output.go:336)", o)
			return false
		},
	))
	codecOpts := []codec.Option{CodecOptionOutputID{OutputID: o.ID}}
	o.TranscoderNode.Processor.Kernel.Decoder.DecoderFactory.ResourceManager = o.asCodecResourceManager()
	o.TranscoderNode.Processor.Kernel.Decoder.DecoderFactory.Options = codecOpts
	o.TranscoderNode.Processor.Kernel.Encoder.EncoderFactory.ResourceManager = o.asCodecResourceManager()
	o.TranscoderNode.Processor.Kernel.Encoder.EncoderFactory.Options = codecOpts
	o.MapIndices.CustomData = customData
	o.SendingFixer.SetCustomData(customData)
	o.SendingSyncer.CustomData = customData
	o.SendingNode.SetCustomData(customData)
	err = configureVideoSenderReconnectPacketFlow(ctx, o.SendingNode, senderKey, o.resetSendingPathForReconnect, func(ctx context.Context) error {
		return o.SetForceNextFrameKey(ctx, true)
	})
	if err != nil {
		return nil, fmt.Errorf("unable to configure video sender reconnect packet flow: %w", err)
	}

	if outputReuseDecoderResources {
		o.initReuseDecoderResources(ctx)
	}
	o.initFPSFractioner(ctx)

	// wiring

	var inputFixer, outputFixer node.Abstract
	inputFixer, outputFixer = o.InputFixer, o.SendingFixer
	if senderKey.VideoCodec == codectypes.Name(codec.NameCopy) {
		inputFixer, outputFixer = o.TranscoderNode, o.SendingSyncer
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

	// Audio-copy guard: when AudioCodec == NameCopy the audio path is a strict
	// packet pass-through (the AutoHeaders BSF and EncoderCopy both reject
	// frames). If an upstream stage (e.g. ffstream's AudioSync kernel)
	// pre-decodes audio packets into frames before pushing them at StreamMux,
	// those frames cannot be re-packetised without transcoding (which would
	// violate "copy" semantics) — let them through and the pipeline aborts
	// with a fatal codec.ErrCopyEncoder / "BitstreamFilter could be used only
	// for Packet-s" error that takes the unrelated video output down with it.
	// Drop audio frames at the InputFilter→inputFixer edge so the audio-copy
	// path stays packet-only and the rest of the pipeline survives.
	if senderKey.AudioCodec == codectypes.Name(codec.NameCopy) {
		audioCopyAcceptsPacketsOnly := o.audioCopyAcceptsPacketsOnly()
		pushToFixerConds = append(pushToFixerConds, audioCopyAcceptsPacketsOnly)
	}
	o.InputFilter.AddPushTo(ctx, inputFixer, packetorframefiltercondition.PacketOrFrame{pushToFixerConds})
	pushToTranscoderConds := packetorframecondition.Function(o.onTranscoderInput)
	o.InputFixer.AddPushTo(ctx, o.TranscoderNode, packetorframefiltercondition.PacketOrFrame{pushToTranscoderConds})
	pushToMapIndicesConds := packetorframecondition.Function(o.onTranscoderOutput)
	o.TranscoderNode.AddPushTo(ctx, o.MapIndices, packetorframefiltercondition.PacketOrFrame{pushToMapIndicesConds})
	o.MapIndices.AddPushTo(ctx, outputFixer)
	pushToSenderConds := packetorframecondition.Function(o.onSenderInput)
	o.SendingFixer.AddPushTo(ctx, o.SendingSyncer, packetorframefiltercondition.PacketOrFrame{pushToSenderConds})
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
	o.SendingSyncer.AddPushTo(ctx, o.SendingNode, packetorframefiltercondition.PacketOrFrame{pushToSendingNodeConds})

	// logging

	logger.Tracef(ctx, "o.InputFilter.Processor.Kernel.Handler.Condition: %p", o.InputFilter.Processor.Kernel.Handler.Condition)
	logger.Tracef(ctx, "o.OutputSyncer.Processor.Kernel.Handler.Condition: %p", o.SendingSyncer.Processor.Kernel.Handler.Condition)

	return o, nil
}

func configureVideoSenderReconnectKeyFrameRequest[C any](
	ctx context.Context,
	sendingNode SendingNode[C],
	senderKey SenderKey,
	requestKeyFrame func(context.Context) error,
) error {
	return configureVideoSenderReconnectPacketFlow(ctx, sendingNode, senderKey, nil, requestKeyFrame)
}

func configureVideoSenderReconnectPacketFlow[C any](
	ctx context.Context,
	sendingNode SendingNode[C],
	senderKey SenderKey,
	resetPacketFlow func(context.Context) error,
	requestKeyFrame func(context.Context) error,
) error {
	if senderKey.VideoCodec == "" {
		return nil
	}
	if requestKeyFrame == nil {
		return errors.New("key frame request callback is nil")
	}

	kerneler, ok := sendingNode.GetProcessor().(processor.GetKerneler)
	if !ok {
		return nil
	}
	retryableOutput, ok := kerneler.GetKernel().(*kernel.Retryable[*kernel.Output])
	if !ok {
		return nil
	}

	if !retryableOutput.KernelLocker.ManualLock(ctx) {
		return ctx.Err()
	}
	defer retryableOutput.KernelLocker.ManualUnlock(ctx)

	previousOnKernelOpen := retryableOutput.Config.OnKernelOpen
	var outputCount atomic.Uint64
	retryableOutput.Config.OnKernelOpen = func(
		ctx context.Context,
		output *kernel.Output,
	) error {
		if previousOnKernelOpen != nil {
			if err := previousOnKernelOpen(ctx, output); err != nil {
				return err
			}
		}
		if output == nil {
			return nil
		}

		shouldResetPacketFlow := outputCount.Add(1) > 1
		previousOnReady := output.Config.OnReady
		output.Config.OnReady = func(
			ctx context.Context,
			output *kernel.Output,
		) error {
			if previousOnReady != nil {
				if err := previousOnReady(ctx, output); err != nil {
					return err
				}
			}
			if shouldResetPacketFlow && resetPacketFlow != nil {
				if err := resetPacketFlow(ctx); err != nil {
					return fmt.Errorf("unable to reset video sender packet flow: %w", err)
				}
			}
			if err := requestKeyFrame(ctx); err != nil {
				return fmt.Errorf("unable to request video key frame: %w", err)
			}
			return nil
		}
		if err := requestKeyFrame(ctx); err != nil {
			return fmt.Errorf("unable to request video key frame after output open: %w", err)
		}
		return nil
	}
	return nil
}

func (o *Output[C]) resetSendingPathForReconnect(
	ctx context.Context,
) error {
	resetters := []struct {
		name string
		node node.Abstract
	}{
		{"MapIndices", o.MapIndices},
		{"SendingFixerInput", o.SendingFixer.Input()},
		{"SendingFixerOutput", o.SendingFixer.Output()},
		{"SendingSyncer", o.SendingSyncer},
		{"SendingNode", o.SendingNode},
	}

	var errs []error
	for _, item := range resetters {
		if item.node == nil || item.node.GetProcessor() == nil {
			continue
		}
		resetter, ok := item.node.GetProcessor().(kerneltypes.Resetter)
		if !ok {
			continue
		}
		if err := resetter.Reset(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to reset %s: %w", item.name, err))
		}
	}
	return errors.Join(errs...)
}

func logIfError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	logger.Errorf(ctx, "got an error: %v", err)
}

func (o *Output[C]) onTranscoderInput(
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
		o.Measurements.TranscodingStartVideoDTS.Store(uint64(dts))
	case astiav.MediaTypeAudio:
		o.Measurements.TranscodingStartAudioDTS.Store(uint64(dts))
	}
	return true
}

func (o *Output[C]) onTranscoderOutput(
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
		o.Measurements.TranscodingEndVideoDTS.Store(uint64(dts))
	case astiav.MediaTypeAudio:
		o.Measurements.TranscodingEndAudioDTS.Store(uint64(dts))
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
	return o.TranscoderNode
}

func (o *Output[C]) initFPSFractioner(ctx context.Context) {
	logger.Tracef(ctx, "initFPSFractioner()")
	defer func() { logger.Tracef(ctx, "/initFPSFractioner()") }()

	if o.FPSFractionGetter == nil {
		return
	}

	o.TranscoderNode.Processor.Kernel.FilterCondition = framecondition.Or{
		framecondition.Not{framecondition.MediaType(astiav.MediaTypeVideo)},
		frameconditionextra.PacketOrFrame{
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

	encRes := *o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoResolution

	// use reusable (by encoder) pixel_format without offloading to CPU
	o.TranscoderNode.Processor.Kernel.DecoderFactory.PreInitFunc = func(
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
	o.IsClosedValue.Store(true)

	var errs []error

	node.AppendInputFilter(ctx, o.FirstNodeAfterFilter(), packetorframefiltercondition.Static(false))
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
	if err := o.TranscoderNode.Processor.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close transcoder node for output %d: %w", o.ID, err))
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
	if o.CancelFn != nil {
		o.CancelFn()
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

	if err := o.TranscoderNode.Processor.Kernel.ResetHard(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to reset transcoder node for output %d: %w", o.ID, err))
	}

	if err := o.SendingNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to deinit sending node for output %d: %w", o.ID, err))
	}

	return errors.Join(errs...)
}

func PartialSenderKeyFromTranscoderConfig(
	ctx context.Context,
	c *types.TranscoderConfig,
) SenderKey {
	if c == nil {
		return SenderKey{}
	}

	var audioCodec codec.Name
	var audioSampleRate audio.SampleRate
	if len(c.Output.AudioTrackConfigs) > 0 {
		audioCfg := c.Output.AudioTrackConfigs[0]
		audioCodec = configuredCodecName(audioCfg.CodecNames, audioCfg.CodecName).Canonicalize(ctx, true)
		audioSampleRate = c.Output.AudioTrackConfigs[0].SampleRate
	}
	var videoCodec codec.Name
	var resolution codec.Resolution
	if len(c.Output.VideoTrackConfigs) > 0 {
		videoCfg := c.Output.VideoTrackConfigs[0]
		videoCodec = configuredCodecName(videoCfg.CodecNames, videoCfg.CodecName).Canonicalize(ctx, true)
		resolution = videoCfg.Resolution
	}
	return SenderKey{
		AudioCodec:      codectypes.Name(audioCodec),
		AudioSampleRate: audioSampleRate,
		VideoCodec:      codectypes.Name(videoCodec),
		VideoResolution: resolution,
	}
}

func (o *Output[C]) reconfigureTranscoder(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureTranscoder(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureTranscoder(ctx, %#+v): %v", cfg, _err) }()

	isCopyEncoder, err := o.reconfigureEncoder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to reconfigure the encoder: %w", err)
	}
	if len(cfg.Output.VideoTrackConfigs) == 0 {
		if err := o.reconfigureFilters(ctx, cfg); err != nil {
			return fmt.Errorf("unable to reconfigure filters: %w", err)
		}
		if err := o.reconfigureMapping(ctx, cfg); err != nil {
			return fmt.Errorf("unable to reconfigure mapping: %w", err)
		}
		o.TranscoderNode.SetInputFilter(ctx, nil)
		return nil
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0]
	if configuredCodecName(videoCfg.CodecNames, videoCfg.CodecName) == codec.NameCopy && !isCopyEncoder {
		logger.Errorf(ctx, "the encoder is not a copy encoder despite it should be")
		isCopyEncoder = true
	}
	if !isCopyEncoder {
		if err := o.reconfigureDecoder(ctx, cfg); err != nil {
			return fmt.Errorf("unable to reconfigure the decoder: %w", err)
		}
	}

	if err := o.reconfigureFilters(ctx, cfg); err != nil {
		return fmt.Errorf("unable to reconfigure filters: %w", err)
	}

	if err := o.reconfigureMapping(ctx, cfg); err != nil {
		return fmt.Errorf("unable to reconfigure mapping: %w", err)
	}

	o.TranscoderNode.SetInputFilter(ctx, nil)
	return nil
}

func (o *Output[C]) reconfigureMapping(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureMapping(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureMapping(ctx, %#+v): %v", cfg, _err) }()

	mapping := make(map[int]int)
	for _, videoCfg := range cfg.Output.VideoTrackConfigs {
		for i, inIdx := range videoCfg.InputTrackIDs {
			if i < len(videoCfg.OutputTrackIDs) {
				mapping[inIdx] = videoCfg.OutputTrackIDs[i]
			}
		}
	}
	for _, audioCfg := range cfg.Output.AudioTrackConfigs {
		for i, inIdx := range audioCfg.InputTrackIDs {
			if i < len(audioCfg.OutputTrackIDs) {
				mapping[inIdx] = audioCfg.OutputTrackIDs[i]
			}
		}
	}

	if len(mapping) == 0 {
		return nil
	}

	mapNode := o.MapIndices.Processor.Kernel
	assigner, ok := mapNode.Assigner.(*streamIndexAssigner)
	if !ok {
		return fmt.Errorf("MapIndices assigner is not *streamIndexAssigner (it is %T)", mapNode.Assigner)
	}

	// TODO: notify assigned about the mapping
	_ = assigner

	return nil
}

func (o *Output[C]) reconfigureFilters(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureFilters(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureFilters(ctx, %#+v): %v", cfg, _err) }()

	transcoder := o.TranscoderNode.Processor.Kernel

	trackConfigs := make(map[int]avfilter.TrackConfig)
	inputFmtCtx := o.InputFrom.Processor.Kernel.FormatContext
	for _, stream := range inputFmtCtx.Streams() {
		mediaType := stream.CodecParameters().MediaType()
		var filters []string
		switch mediaType {
		case astiav.MediaTypeVideo:
			if len(cfg.Output.VideoTrackConfigs) > 0 {
				filters = cfg.Output.VideoTrackConfigs[0].Filters
			}
		case astiav.MediaTypeAudio:
			if len(cfg.Output.AudioTrackConfigs) > 0 {
				filters = cfg.Output.AudioTrackConfigs[0].Filters
			}
		}
		trackConfigs[stream.Index()] = avfilter.TrackConfig{
			Filters:         filters,
			CodecParameters: stream.CodecParameters(),
			TimeBase:        stream.TimeBase(),
		}
	}

	if cfg.Output.FilterComplex == "" {
		// no filters
		oldFilterKernel := transcoder.GetFilterKernel(ctx)
		if oldFilterKernel != nil {
			_ = oldFilterKernel.Close(ctx)
		}
		transcoder.SetFilterKernel(ctx, nil)
		return nil
	}

	g, err := avfilter.NewGraph(ctx, trackConfigs, cfg.Output.FilterComplex)
	if err != nil {
		return fmt.Errorf("unable to create a new filter graph: %w", err)
	}

	filterGraph := kernel.NewAVFilterGraph(ctx, g)

	oldFilterKernel := transcoder.GetFilterKernel(ctx)
	if oldFilterKernel != nil {
		_ = oldFilterKernel.Close(ctx)
	}
	transcoder.SetFilterKernel(ctx, filterGraph)

	return nil
}

func (o *Output[C]) reconfigureDecoder(
	ctx context.Context,
	cfg types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "reconfigureDecoder: %#+v", cfg)
	defer func() { logger.Tracef(ctx, "/reconfigureDecoder: %#+v: %v", cfg, _err) }()
	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (received a request for %d track configs)", len(cfg.Output.VideoTrackConfigs))
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0]
	decoderVideoCfg := inputVideoTrackConfig(cfg)
	decoderHardwareDeviceType := types.HardwareDeviceType(decoderVideoCfg.HardwareDeviceType)
	decoderHardwareDeviceName := types.HardwareDeviceName(decoderVideoCfg.HardwareDeviceName)
	decoderCustomOptions := decoderVideoCfg.CustomOptions
	if cfg.Input == nil || len(cfg.Input.VideoTrackConfigs) == 0 {
		decoderHardwareDeviceType = videoCfg.GetDecoderHardwareDeviceType()
		decoderHardwareDeviceName = videoCfg.GetDecoderHardwareDeviceName()
		decoderCustomOptions = videoCfg.CustomOptions
	}

	var videoOptions globaltypes.DictionaryItems
	for _, opt := range convertCustomOptions(decoderCustomOptions) {
		switch opt.Key {
		case "create_window":
			videoOptions = append(videoOptions, opt)
		case "pixel_format":
			videoOptions = append(videoOptions, opt)
		}
	}

	decoder := o.TranscoderNode.Processor.Kernel.Decoder
	decoderFactory := decoder.DecoderFactory

	err := xsync.DoR1(ctx, &decoder.Locker, func() error {
		if len(decoder.Decoders) == 0 {
			logger.Debugf(ctx, "the decoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")
			decoderFactory.VideoCodec = configuredCodecName(decoderVideoCfg.CodecNames, decoderVideoCfg.CodecName)
			decoderFactory.VideoCodecs = codecNames(decoderVideoCfg.CodecNames)
			decoderFactory.HardwareDeviceType = decoderHardwareDeviceType
			decoderFactory.HardwareDeviceName = codec.HardwareDeviceName(decoderHardwareDeviceName)
			decoderFactory.VideoOptions = xastiav.DictionaryItemsToAstiav(ctx, videoOptions)
			return nil
		}
		if decoderHardwareDeviceType != types.HardwareDeviceType(decoderFactory.HardwareDeviceType) {
			return fmt.Errorf("unable to change the decoding hardware device type on the fly, yet: '%s' != '%s'", decoderHardwareDeviceType, decoderFactory.HardwareDeviceType)
		}
		if decoderHardwareDeviceName != types.HardwareDeviceName(decoderFactory.HardwareDeviceName) {
			return fmt.Errorf("unable to change the decoding hardware device name on the fly, yet: '%s' != '%s'", decoderHardwareDeviceName, decoderFactory.HardwareDeviceName)
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
	cfg types.TranscoderConfig,
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
	hasVideoCfg := len(cfg.Output.VideoTrackConfigs) > 0

	var audioCfg types.OutputAudioTrackConfig
	if len(cfg.Output.AudioTrackConfigs) > 1 {
		return false, fmt.Errorf("currently we support only one output audio track config (received a request for %d track configs)", len(cfg.Output.AudioTrackConfigs))
	}
	if len(cfg.Output.AudioTrackConfigs) > 0 {
		audioCfg = cfg.Output.AudioTrackConfigs[0]
	}
	hasAudioCfg := len(cfg.Output.AudioTrackConfigs) > 0

	encoderFactory := o.TranscoderNode.Processor.Kernel.EncoderFactory
	configuredVideoCodecName := configuredCodecName(videoCfg.CodecNames, videoCfg.CodecName)

	var videoOptions globaltypes.DictionaryItems
	videoOptions = append(videoOptions, globaltypes.DictionaryItems{
		{Key: "forced-idr", Value: "1"},    // to avoid corruptions on switching the outputs
		{Key: "intra-refresh", Value: "0"}, // to avoid corruptions on switching the outputs
	}...)
	videoOptions = append(videoOptions, convertCustomOptions(videoCfg.CustomOptions)...)
	videoOptions = forceRawFrameSourceMediaCodecPixFmt(
		ctx,
		videoOptions,
		o.RawFrameSource.Load(),
		types.HardwareDeviceType(videoCfg.HardwareDeviceType),
		configuredVideoCodecName,
	)

	err := xsync.DoR1(ctx, &encoderFactory.Locker, func() error {
		if len(encoderFactory.VideoEncoders) == 0 && len(encoderFactory.AudioEncoders) == 0 {
			logger.Debugf(ctx, "the encoder is not yet initialized, so asking it to have the correct settings when it will be being initialized")

			if hasVideoCfg {
				encoderFactory.VideoCodec = configuredVideoCodecName
				encoderFactory.VideoCodecs = codecNames(videoCfg.CodecNames)
				_isCopyEncoder = encoderFactory.VideoCodec == codec.NameCopy
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
				// Rescale to millisecond-precision denominator to avoid rounding artifacts
				// (e.g. 30/1 becomes 30000/1000).
				newNum := fps.Num * 1000 / fps.Den
				encoderFactory.VideoAverageFrameRate = astiav.NewRational(newNum, 1000)
			} else {
				encoderFactory.VideoCodec = ""
				encoderFactory.VideoCodecs = nil
				encoderFactory.VideoOptions = nil
				encoderFactory.VideoQuality = nil
				encoderFactory.VideoResolution = nil
				encoderFactory.VideoAverageFrameRate = astiav.Rational{}
				encoderFactory.HardwareDeviceName = ""
				encoderFactory.HardwareDeviceType = globaltypes.HardwareDeviceTypeNone
			}

			if hasAudioCfg {
				encoderFactory.AudioCodec = configuredCodecName(audioCfg.CodecNames, audioCfg.CodecName)
				encoderFactory.AudioCodecs = codecNames(audioCfg.CodecNames)
				encoderFactory.AudioOptions = xastiav.DictionaryItemsToAstiav(ctx, convertCustomOptions(audioCfg.CustomOptions))
				if audioCfg.AverageBitRate != 0 {
					encoderFactory.AudioQuality = quality.ConstantBitrate(audioCfg.AverageBitRate)
				}
				encoderFactory.AudioSampleRate = audioCfg.SampleRate
				encoderFactory.AudioChannels = audioCfg.Channels
			} else {
				encoderFactory.AudioCodec = ""
				encoderFactory.AudioCodecs = nil
				encoderFactory.AudioOptions = nil
				encoderFactory.AudioQuality = nil
				encoderFactory.AudioSampleRate = 0
				encoderFactory.AudioChannels = 0
			}
			return nil
		}

		if !hasVideoCfg {
			if len(encoderFactory.VideoEncoders) > 0 {
				return fmt.Errorf("unable to remove active video encoder on the fly")
			}
		}
		if hasVideoCfg && !codecNamesEqual(configuredCodecNames(videoCfg.CodecNames, videoCfg.CodecName), encoderFactoryCodecNames(encoderFactory.VideoCodecs, encoderFactory.VideoCodec)) {
			return fmt.Errorf("unable to change the encoding codec on the fly, yet: '%v' != '%v'", configuredCodecNames(videoCfg.CodecNames, videoCfg.CodecName), encoderFactoryCodecNames(encoderFactory.VideoCodecs, encoderFactory.VideoCodec))
		}

		logger.Debugf(ctx, "the encoder is already initialized, so modifying it if needed")
		if hasVideoCfg && len(encoderFactory.VideoEncoders) == 0 {
			return fmt.Errorf("unable to configure video track without an active video encoder")
		}
		var encoder codec.Encoder
		if hasVideoCfg {
			encoder = encoderFactory.VideoEncoders[0]
			_isCopyEncoder = codec.IsEncoderCopy(encoder)
		}

		if hasVideoCfg && videoCfg.HardwareDeviceType != types.HardwareDeviceType(encoderFactory.HardwareDeviceType) {
			return fmt.Errorf("unable to change the hardware device type on the fly, yet: '%s' != '%s'", videoCfg.HardwareDeviceType, encoderFactory.HardwareDeviceType)
		}

		if hasVideoCfg && videoCfg.HardwareDeviceName != types.HardwareDeviceName(encoderFactory.HardwareDeviceName) {
			return fmt.Errorf("unable to change the hardware device name on the fly, yet: '%s' != '%s'", videoCfg.HardwareDeviceName, encoderFactory.HardwareDeviceName)
		}

		if !hasAudioCfg {
			if len(encoderFactory.AudioEncoders) > 0 {
				return fmt.Errorf("unable to remove active audio encoder on the fly")
			}
		}

		if hasAudioCfg && audioCfg.SampleRate != encoderFactory.AudioSampleRate {
			return fmt.Errorf("unable to change the audio sample rate on the fly, yet: '%d' != '%d'", audioCfg.SampleRate, encoderFactory.AudioSampleRate)
		}

		if hasAudioCfg && audioCfg.Channels != encoderFactory.AudioChannels {
			return fmt.Errorf("unable to change the audio channels on the fly, yet: '%d' != '%d'", audioCfg.Channels, encoderFactory.AudioChannels)
		}

		if !hasVideoCfg {
			return nil
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

func inputVideoTrackConfig(cfg types.TranscoderConfig) types.InputVideoTrackConfig {
	if cfg.Input == nil || len(cfg.Input.VideoTrackConfigs) == 0 {
		return types.InputVideoTrackConfig{}
	}
	return cfg.Input.VideoTrackConfigs[0]
}

func codecNames(names []codectypes.Name) []codec.Name {
	if len(names) == 0 {
		return nil
	}
	result := make([]codec.Name, 0, len(names))
	for _, name := range names {
		result = append(result, codec.Name(name))
	}
	return result
}

func configuredCodecNames(
	names []codectypes.Name,
	codecName codectypes.Name,
) []codec.Name {
	if len(names) > 0 {
		return codecNames(names)
	}
	if codecName == "" {
		return nil
	}
	return []codec.Name{codec.Name(codecName)}
}

func configuredCodecName(
	names []codectypes.Name,
	codecName codectypes.Name,
) codec.Name {
	configuredNames := configuredCodecNames(names, codecName)
	if len(configuredNames) == 0 {
		return ""
	}
	return configuredNames[0]
}

func encoderFactoryCodecNames(
	names []codec.Name,
	codecName codec.Name,
) []codec.Name {
	if len(names) > 0 {
		return slices.Clone(names)
	}
	if codecName == "" {
		return nil
	}
	return []codec.Name{codecName}
}

func encoderFactoryPrimaryCodec(
	names []codec.Name,
	codecName codec.Name,
) codec.Name {
	configuredNames := encoderFactoryCodecNames(names, codecName)
	if len(configuredNames) == 0 {
		return ""
	}
	return configuredNames[0]
}

func codecNamesEqual(a, b []codec.Name) bool {
	return slices.Equal(a, b)
}

func (o *Output[C]) GetKey() SenderKey {
	var videoResolution codec.Resolution
	if o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoResolution != nil {
		videoResolution = *o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoResolution
	}
	ctx := context.Background()
	return SenderKey{
		AudioCodec:      canonicalizeCodecName(ctx, encoderFactoryPrimaryCodec(o.TranscoderNode.Processor.Kernel.EncoderFactory.AudioCodecs, o.TranscoderNode.Processor.Kernel.EncoderFactory.AudioCodec)),
		AudioSampleRate: o.TranscoderNode.Processor.Kernel.EncoderFactory.AudioSampleRate,
		VideoCodec:      canonicalizeCodecName(ctx, encoderFactoryPrimaryCodec(o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoCodecs, o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoCodec)),
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
		o.TranscoderNode,
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
	return o.IsClosedValue.Load()
}

func (o *Output[C]) SetForceNextFrameKey(
	ctx context.Context,
	forceNextFrameKey bool,
) (_err error) {
	logger.Tracef(ctx, "Output[%d].SetForceNextFrameKey(%v)", o.ID, forceNextFrameKey)
	defer func() { logger.Tracef(ctx, "/Output[%d].SetForceNextFrameKey(%v): %v", o.ID, forceNextFrameKey, _err) }()

	return o.TranscoderNode.Processor.Kernel.Encoder.SetForceNextKeyFrame(ctx, forceNextFrameKey)
}
