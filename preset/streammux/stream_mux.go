package streammux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/dustin/go-humanize"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	nodeboilerplate "github.com/xaionaro-go/avpipeline/node/boilerplate"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
	"tailscale.com/util/ringbuffer"
)

const (
	switchTimeout = time.Hour
	switchDebug   = false
)

var (
	EnableDraining = false // TODO: enable this (currently it causes bugs with mediacodec)
)

type StreamMux[C any] struct {
	MuxMode            types.MuxMode
	CurrentOutputProps types.SenderProps
	Locker             xsync.Mutex

	// inputs:
	InputAll       Input[C]
	InputAudioOnly *Input[C]
	InputVideoOnly *Input[C]

	// outputs:
	Outputs       []*Output[C]
	OutputsMap    xsync.Map[SenderKey, *Output[C]]
	OutputsLocker xsync.Mutex
	SenderFactory SenderFactory[C]

	// aux
	AutoBitRateHandler      *AutoBitRateHandler[C]
	FPSFractionNumDen       atomic.Uint64
	VideoOutputBeforeBypass *Output[C]

	// measurements
	CurrentVideoInputBitRate        atomic.Uint64
	CurrentVideoEncodedBitRate      atomic.Uint64
	CurrentVideoOutputBitRate       atomic.Uint64
	CurrentAudioInputBitRate        atomic.Uint64
	CurrentAudioEncodedBitRate      atomic.Uint64
	CurrentAudioOutputBitRate       atomic.Uint64
	CurrentOtherInputBitRate        atomic.Uint64
	CurrentOtherEncodedBitRate      atomic.Uint64
	CurrentOtherOutputBitRate       atomic.Uint64
	CurrentBitRateMeasurementsCount atomic.Uint64

	// to become a fake node myself:
	nodeboilerplate.Counters
	nodeboilerplate.InputFilter

	// private:
	startedCh     *chan struct{}
	waitGroup     sync.WaitGroup
	lastKeyFrames map[int]*ringbuffer.RingBuffer[packet.Input]
}

func New(
	ctx context.Context,
	muxMode types.MuxMode,
	autoBitRate *AutoBitRateConfig,
	senderFactory SenderFactory[struct{}],
) (*StreamMux[struct{}], error) {
	return NewWithCustomData[struct{}](
		ctx,
		muxMode,
		autoBitRate,
		senderFactory,
	)
}

func NewWithCustomData[C any](
	ctx context.Context,
	muxMode types.MuxMode,
	autoBitRate *AutoBitRateConfig,
	senderFactory SenderFactory[C],
) (*StreamMux[C], error) {
	s := &StreamMux[C]{
		SenderFactory: senderFactory,
		MuxMode:       muxMode,

		lastKeyFrames: map[int]*ringbuffer.RingBuffer[packet.Input]{},
		startedCh:     ptr(make(chan struct{})),
	}
	s.InputAll = *newInput[C](ctx, s, InputTypeAll)
	if muxMode == types.MuxModeDifferentOutputsSameTracksSplitAV {
		s.InputAudioOnly = newInput[C](ctx, s, InputTypeAudioOnly)
		s.InputVideoOnly = newInput[C](ctx, s, InputTypeVideoOnly)
		s.InputAll.Node.AddPushPacketsTo(s.InputAudioOnly.Node, packetfiltercondition.MediaType(astiav.MediaTypeAudio))
		s.InputAll.Node.AddPushPacketsTo(s.InputVideoOnly.Node, packetfiltercondition.MediaType(astiav.MediaTypeVideo))
	}
	s.CurrentAudioInputBitRate.Store(192_000) // some reasonable high end guess
	s.CurrentOtherInputBitRate.Store(0)
	if err := s.initSwitches(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize switches: %w", err)
	}

	if autoBitRate != nil {
		logger.Tracef(ctx, "enabling automatic bitrate control")
		if len(autoBitRate.ResolutionsAndBitRates) == 0 {
			return nil, fmt.Errorf("at least one resolution must be specified for automatic bitrate control")
		}
		h := s.initAutoBitRateHandler(*autoBitRate)
		observability.Go(ctx, func(ctx context.Context) {
			defer logger.Debugf(ctx, "autoBitRateHandler.ServeContext(): done")
			err := h.ServeContext(ctx)
			logger.Debugf(ctx, "autoBitRateHandler.ServeContext(): %v", err)
		})
		bestCfg := autoBitRate.ResolutionsAndBitRates.Best()
		s.CurrentVideoInputBitRate.Store(uint64(bestCfg.BitrateHigh))
	} else {
		s.CurrentVideoInputBitRate.Store(math.MaxUint64)
	}

	return s, nil
}

func (s *StreamMux[C]) ForEachInput(
	ctx context.Context,
	fn func(ctx context.Context, input *Input[C]) error,
) (_err error) {
	logger.Tracef(ctx, "ForEachInput")
	defer func() { logger.Tracef(ctx, "/ForEachInput: %v", _err) }()
	for _, input := range []*Input[C]{
		&s.InputAll,
		s.InputAudioOnly,
		s.InputVideoOnly,
	} {
		input := input
		if input == nil {
			continue
		}
		err := fn(ctx, input)
		switch {
		case err == nil:
		case err == ErrStop{}:
			return nil
		default:
			return fmt.Errorf("unable to process input %s: %w", input.GetType(), err)
		}
	}
	return nil
}

func (s *StreamMux[C]) initSwitches(
	ctx context.Context,
) error {
	switch s.MuxMode {
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		// we don't consume directly from s.InputAll in this mode
		if err := s.InputAll.OutputSwitch.SetValue(ctx, -1); err != nil {
			return fmt.Errorf("unable to initialize switch for catch-all input: %w", err)
		}
		if err := s.InputAll.OutputSyncer.SetValue(ctx, -1); err != nil {
			return fmt.Errorf("unable to initialize switch for catch-all input: %w", err)
		}
	}
	return s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		inputType := input.GetType()
		if inputType.IncludesMediaType(astiav.MediaTypeVideo) {
			input.OutputSwitch.SetKeepUnless(packetorframecondition.And{
				packetorframecondition.MediaType(astiav.MediaTypeVideo),
				packetorframecondition.IsKeyFrame(true),
			})
		}

		if switchDebug {
			input.OutputSwitch.SetOnBeforeSwitch(func(
				ctx context.Context,
				in packetorframe.InputUnion,
				from, to int32,
			) {
				logger.Debugf(ctx,
					"Switch[%s].SetOnBeforeSwitch: %d -> %d",
					inputType, from, to,
				)
			})
		}

		input.OutputSwitch.SetOnAfterSwitch(func(
			ctx context.Context,
			in packetorframe.InputUnion,
			from, to int32,
		) {
			logger.Debugf(ctx, "Switch[%s].SetOnAfterSwitch: %d -> %d", inputType, from, to)

			logger.Debugf(ctx, "Syncer[%s].SetValue(ctx, %d): from %d", inputType, to, from)
			err := input.OutputSyncer.SetValue(ctx, to)
			logger.Debugf(ctx, "/Syncer[%s].SetValue(ctx, %d): from %d: %v", inputType, to, from, err)

			s.OutputsLocker.ManualLock(ctx)
			outputNext := s.Outputs[to]
			if err := outputNext.SetForceNextFrameKey(ctx, true); err != nil {
				logger.Errorf(ctx, "Switch[%s]: unable to set force key frame on the output %d: %v", inputType, to, err)
			}
			observability.Go(ctx, func(ctx context.Context) {
				defer s.OutputsLocker.ManualUnlock(ctx)
				outputPrev := s.Outputs[from]

				if err := node.RemovePushPacketsTo(ctx, outputPrev.InputFrom, outputPrev.Input()); err != nil {
					logger.Errorf(ctx, "Switch[%s]: unable to remove push packets to the output %d: %v", inputType, from, err)
				}

				if EnableDraining {
					ctx, cancelFn := context.WithTimeout(ctx, switchTimeout)
					defer cancelFn()
					err := outputPrev.Drain(ctx)
					if err != nil {
						logger.Errorf(ctx, "Switch[%s]: unable to close the output %d: %v", inputType, from, err)
					}
				}

				err := outputPrev.CloseNoDrain(ctx)
				if err != nil {
					logger.Errorf(ctx, "Switch[%s]: unable to close the output %d: %v", inputType, from, err)
				}

				outputPrev.FirstNodeAfterFilter().SetInputPacketFilter(packetfiltercondition.Panic("Switch[" + inputType.String() + "]: somehow received a packet, while the output is closed"))

				observability.Go(ctx, func(ctx context.Context) {
					s.Locker.Do(ctx, func() {
						err := s.createAndConfigureOutputs(ctx, outputPrev.GetKey(), s.CurrentOutputProps.RecoderConfig)
						if err != nil {
							logger.Errorf(ctx, "Switch[%s]: unable to re-initialize the output %d:%s: %v", inputType, outputPrev.ID, outputPrev.GetKey(), err)
						}
					})
				})
			})
		})
		input.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagInactiveBlock)

		logger.Tracef(ctx, "Switch[%s]: %p", inputType, input.OutputSwitch)
		logger.Tracef(ctx, "Syncer[%s]: %p", inputType, input.OutputSyncer)
		return nil
	})
}

func (s *StreamMux[C]) removeOutputByIDLocked(
	ctx context.Context,
	outputID OutputID,
) (_err error) {
	logger.Tracef(ctx, "removeOutputByIDLocked(%v)", outputID)
	defer func() { logger.Tracef(ctx, "/removeOutputByIDLocked(%v): %v", outputID, _err) }()
	output := s.Outputs[outputID]
	return s.removeOutputLocked(ctx, output.GetKey())
}

var _ = (*StreamMux[struct{}])(nil).removeOutputByIDLocked

func (s *StreamMux[C]) removeOutputLocked(
	ctx context.Context,
	outputKey SenderKey,
) (_err error) {
	logger.Tracef(ctx, "removeOutputLocked(%v)", outputKey)
	defer func() { logger.Tracef(ctx, "/removeOutputLocked(%v): %v", outputKey, _err) }()
	output, ok := s.OutputsMap.LoadAndDelete(outputKey)
	if !ok {
		return fmt.Errorf("output %v not found in OutputsMap", outputKey)
	}
	s.Outputs[output.ID] = s.Outputs[len(s.Outputs)-1]
	s.Outputs[output.ID].ID = output.ID
	s.Outputs = s.Outputs[:len(s.Outputs)-1]
	return nil
}

var _ = (*StreamMux[struct{}])(nil).removeOutputLocked

func (s *StreamMux[C]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "StreamMux.Close()")
	defer func() { logger.Debugf(ctx, "/StreamMux.Close(): %v", _err) }()
	var errs []error

	if err := s.InputAll.Node.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close the input node: %w", err))
	}
	if s.InputAudioOnly != nil {
		if err := s.InputAudioOnly.Node.GetProcessor().Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the audio-only input node: %w", err))
		}
	}
	if s.InputVideoOnly != nil {
		if err := s.InputVideoOnly.Node.GetProcessor().Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the video-only input node: %w", err))
		}
	}
	s.OutputsLocker.Do(ctx, func() {
		s.OutputsMap.Range(func(key SenderKey, output *Output[C]) bool {
			if err := output.Close(ctx); err != nil {
				errs = append(errs, fmt.Errorf("unable to close the output chain %d: %w", output.ID, err))
			}
			return true
		})
	})
	if s.AutoBitRateHandler != nil {
		if err := s.AutoBitRateHandler.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close auto bitrate handler: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *StreamMux[C]) getVideoInput() *Input[C] {
	if s.InputVideoOnly != nil {
		return s.InputVideoOnly
	}
	return &s.InputAll
}

func (s *StreamMux[C]) getAudioInput() *Input[C] {
	if s.InputAudioOnly != nil {
		return s.InputAudioOnly
	}
	return &s.InputAll
}

func (s *StreamMux[C]) setPreferredOutputs(
	ctx context.Context,
	senderKey SenderKey,
) (_err error) {
	logger.Debugf(ctx, "setPreferredOutputs(ctx, %s)", senderKey)
	defer func() { logger.Debugf(ctx, "/setPreferredOutputs(ctx, %s): %v", senderKey, _err) }()

	switch s.MuxMode {
	case types.MuxModeDifferentOutputsSameTracks:
		err := s.setPreferredOutputForInput(ctx, &s.InputAll, senderKey)
		if err != nil {
			return fmt.Errorf("unable to set preferred output for all input with key %s: %w", senderKey, err)
		}
		return nil
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		var alreadyPreferredErr0, alreadyPreferredErr1 ErrOutputAlreadyPreferred
		alreadyPreferredCount := 0
		outputsToChange := 0
		if senderKey.VideoCodec != "" {
			outputsToChange++
			senderKeyVideo := SenderKey{
				VideoCodec:      senderKey.VideoCodec,
				VideoResolution: senderKey.VideoResolution,
			}
			err := s.setPreferredOutputForInput(ctx, s.getVideoInput(), senderKeyVideo)
			switch {
			case err == nil:
			case errors.As(err, &alreadyPreferredErr0):
				alreadyPreferredCount++
			default:
				return fmt.Errorf("unable to set preferred output for video input with key %s: %w", senderKeyVideo, err)
			}
		}
		if senderKey.AudioCodec != "" {
			outputsToChange++
			senderKeyAudio := SenderKey{
				AudioCodec:      senderKey.AudioCodec,
				AudioSampleRate: senderKey.AudioSampleRate,
			}
			err := s.setPreferredOutputForInput(ctx, s.getAudioInput(), senderKeyAudio)
			switch {
			case err == nil:
			case errors.Is(err, &alreadyPreferredErr1):
				alreadyPreferredCount++
			default:
				return fmt.Errorf("unable to set preferred output for audio input with key %v: %w", senderKeyAudio, err)
			}
		}
		if alreadyPreferredCount >= outputsToChange {
			return ErrOutputsAlreadyPreferred{
				OutputIDs: []OutputID{alreadyPreferredErr0.OutputID, alreadyPreferredErr1.OutputID},
			}
		}
		return nil
	default:
		return fmt.Errorf("unable to set preferred output in mux mode %s", s.MuxMode)
	}
}

func (s *StreamMux[C]) setPreferredOutputForInput(
	ctx context.Context,
	input *Input[C],
	outputKey SenderKey,
) (_err error) {
	inputType := input.GetType()
	logger.Debugf(ctx, "setPreferredOutputForInput(ctx, %s, %s)", inputType, outputKey)
	defer func() { logger.Debugf(ctx, "/setPreferredOutputForInput(ctx, %s, %s): %v", inputType, outputKey, _err) }()

	output, _ := s.OutputsMap.Load(outputKey)
	if output == nil {
		return fmt.Errorf("output with key %s not found", outputKey)
	}

	// the order is important to avoid race conditions:
	id1 := OutputID(input.OutputSyncer.GetValue(ctx))
	id0 := OutputID(input.OutputSwitch.GetValue(ctx))
	if id0 != id1 {
		return ErrSwitchAlreadyInProgress{OutputIDCurrent: id0, OutputIDNext: id1}
	}
	if output.ID == id0 {
		return ErrOutputAlreadyPreferred{OutputID: output.ID}
	}

	err := input.OutputSwitch.SetValue(ctx, int32(output.ID))
	if err != nil {
		return fmt.Errorf("unable to switch to the preferred output %d:%s: %w", output.ID, outputKey, err)
	}

	return nil
}

func (s *StreamMux[C]) GetOrCreateOutput(
	ctx context.Context,
	outputKey types.SenderKey,
	opts ...InitOutputOption,
) (output *Output[C], _err error) {
	logger.Tracef(ctx, "GetOrCreateOutput(%#+v)", outputKey)
	defer func() { logger.Tracef(ctx, "/GetOrCreateOutput(%#+v): %v", outputKey, _err) }()
	return xsync.DoA3R2(ctx, &s.OutputsLocker, s.getOrCreateOutputLocked, ctx, outputKey, opts)
}

func (s *StreamMux[C]) getOrCreateOutputLocked(
	ctx context.Context,
	outputKey types.SenderKey,
	opts []InitOutputOption,
) (_ret *Output[C], _err error) {
	logger.Debugf(ctx, "getOrCreateOutputLocked: %#+v, %#+v", outputKey, opts)
	defer func() {
		logger.Debugf(ctx, "/getOrCreateOutputLocked: %#+v, %#+v: %v, %v", outputKey, opts, _ret, _err)
	}()

	if outputKey.VideoResolution == (codec.Resolution{}) && outputKey.VideoCodec != "" && outputKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output resolution is not set (codec: %s)", outputKey.VideoCodec)
	}
	if outputKey.AudioSampleRate == 0 && outputKey.AudioCodec != "" && outputKey.AudioCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output audio sample rate is not set (codec: %s)", outputKey.AudioCodec)
	}

	outputID := OutputID(len(s.Outputs))

	if output, ok := s.OutputsMap.Load(outputKey); ok {
		if !output.IsClosed() {
			return output, nil
		}
		outputID = output.ID
	}

	var input *Input[C]
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
	case types.MuxModeDifferentOutputsSameTracks:
		input = &s.InputAll
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		switch {
		case outputKey.AudioCodec != "" && outputKey.VideoCodec != "":
			return nil, fmt.Errorf("in mux mode '%s', you can get video xor audio output, not both (acodec:%s, vcodec:%s)", s.MuxMode, outputKey.AudioCodec, outputKey.VideoCodec)
		case outputKey.AudioCodec != "":
			input = s.InputAudioOnly
		case outputKey.VideoCodec != "":
			input = s.InputVideoOnly
		default:
			return nil, fmt.Errorf("in mux mode '%s', you must specify either audio or video codec in output key", s.MuxMode)
		}
	}

	cfg := InitOutputOptions(opts).config()
	_ = cfg // currently unused

	output, err := newOutput[C](
		ctx,
		outputID,
		input.Node,
		s.SenderFactory, outputKey,
		input.OutputSwitch.Output(int32(outputID)),
		input.OutputSyncer.Output(int32(outputID)),
		input.MonotonicPTSCondition,
		newStreamIndexAssigner(s.MuxMode, outputID, input.Node.Processor.Kernel),
		s,
		s,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output: %w", err)
	}
	s.OutputsMap.Store(outputKey, output)
	switch {
	case int(outputID) < len(s.Outputs):
		s.Outputs[outputID] = output
	case int(outputID) == len(s.Outputs):
		s.Outputs = append(s.Outputs, output)
	default:
		return nil, fmt.Errorf("internal error: outputID %d is greater than len(s.Outputs) %d", outputID, len(s.Outputs))
	}
	input.Node.AddPushPacketsTo(output.Input())
	err = avpipeline.NotifyAboutPacketSources(ctx, input.Node.Processor.Kernel, output.Input())
	if err != nil {
		logger.Errorf(ctx, "received an error while notifying about packet sources (%s -> %s): %v", input.Node.Processor.Kernel, output.Input(), err)
	}
	logger.Debugf(ctx, "initialized new output %d:%s", output.ID, outputKey)
	return output, nil
}

func (s *StreamMux[C]) EnableVideoRecodingBypass(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "EnableVideoRecodingBypass")
	defer func() { logger.Tracef(ctx, "/EnableVideoRecodingBypass: %v", _err) }()
	return xsync.DoA1R1(ctx, &s.Locker, s.enableVideoRecodingBypassLocked, ctx)
}

func (s *StreamMux[C]) enableVideoRecodingBypassLocked(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "enableVideoRecodingBypassLocked")
	defer func() { logger.Tracef(ctx, "/enableVideoRecodingBypassLocked: %v: %v", _err) }()

	prevVideoOutput := s.GetActiveVideoOutput(ctx)

	var senderKey SenderKey
	var recoderCfg types.RecoderConfig
	switch s.MuxMode {
	case types.MuxModeDifferentOutputsSameTracks:
		senderKey = SenderKey{
			AudioCodec: codectypes.Name(codec.NameCopy),
			VideoCodec: codectypes.Name(codec.NameCopy),
		}
		recoderCfg = types.RecoderConfig{
			Output: types.RecoderOutputConfig{
				VideoTrackConfigs: []types.OutputVideoTrackConfig{{
					CodecName: codectypes.Name(codec.NameCopy),
				}},
				AudioTrackConfigs: []types.OutputAudioTrackConfig{{
					CodecName: codectypes.Name(codec.NameCopy),
				}},
			},
		}
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		senderKey = SenderKey{
			VideoCodec: codectypes.Name(codec.NameCopy),
		}
		recoderCfg = types.RecoderConfig{
			Output: types.RecoderOutputConfig{
				VideoTrackConfigs: []types.OutputVideoTrackConfig{{
					CodecName: codectypes.Name(codec.NameCopy),
				}},
			},
		}
	default:
		return fmt.Errorf("unable to enable video recoding bypass in mux mode %s", s.MuxMode)
	}

	err := s.createAndConfigureOutputs(ctx, senderKey, recoderCfg)
	if err != nil {
		return fmt.Errorf("unable to initialize and prepare outputs for bypass key %s: %w", senderKey, err)
	}

	err = s.setPreferredOutputs(ctx, senderKey)
	switch {
	case err == nil:
	case errors.Is(err, ErrOutputAlreadyPreferred{}):
		return nil
	case errors.Is(err, ErrOutputsAlreadyPreferred{}):
		return nil
	default:
		return fmt.Errorf("unable to set the preferred outputs %d:%s: %w", prevVideoOutput.ID, senderKey, err)
	}

	s.VideoOutputBeforeBypass = prevVideoOutput
	return nil
}

func (s *StreamMux[C]) Input() *NodeInput[C] {
	return s.InputAll.Node
}

func (s *StreamMux[C]) GetRecoderConfig(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	logger.Tracef(ctx, "GetRecoderConfig")
	defer func() { logger.Tracef(ctx, "/GetRecoderConfig: %v", _ret) }()
	if s == nil {
		return types.RecoderConfig{}
	}
	outputConfig := xsync.DoA1R1(ctx, &s.Locker, s.getCurrentOutputPropsLocked, ctx)
	return outputConfig.RecoderConfig
}

func (s *StreamMux[C]) getCurrentOutputPropsLocked(
	ctx context.Context,
) (_ret types.SenderProps) {
	assertNoError(json.Unmarshal(must(json.Marshal(s.CurrentOutputProps)), &_ret)) // deep copy of a poor man
	return
}

func (s *StreamMux[C]) SwitchToOutputByProps(
	ctx context.Context,
	props types.SenderProps,
) (_err error) {
	logger.Tracef(ctx, "SwitchToOutputByProps(ctx, %#+v)", props)
	defer func() { logger.Tracef(ctx, "/SwitchToOutputByProps(ctx, %#+v): %v", props, _err) }()
	return xsync.DoA3R1(ctx, &s.Locker, s.switchToOutputByProps, ctx, props, true)
}

func (s *StreamMux[C]) switchToOutputByProps(
	ctx context.Context,
	props types.SenderProps,
	persistent bool,
) (_err error) {
	logger.Tracef(ctx, "switchToOutputByProps: %#+v, %v, %v", props, persistent)
	defer func() {
		logger.Tracef(ctx, "/switchToOutputByProps: %#+v, %v, %v: %v", props, persistent, _err)
	}()
	senderKey := PartialSenderKeyFromRecoderConfig(ctx, &props.RecoderConfig)

	err := s.createAndConfigureOutputs(ctx, senderKey, props.RecoderConfig)
	if err != nil {
		return fmt.Errorf("unable to initialize and prepare outputs for key %s: %w", senderKey, err)
	}

	err = s.setPreferredOutputs(ctx, senderKey)
	switch {
	case err == nil:
	case errors.Is(err, ErrOutputAlreadyPreferred{}):
		return nil
	case errors.Is(err, ErrOutputsAlreadyPreferred{}):
		return nil
	default:
		return fmt.Errorf("unable to set the preferred outputs %s: %w", senderKey, err)
	}

	if persistent {
		s.CurrentOutputProps = props
	}
	return nil
}

type inputAndKey[C any] struct {
	Input *Input[C]
	Key   SenderKey
}

func (s *StreamMux[C]) getInputsForSenderKey(
	ctx context.Context,
	senderKey SenderKey,
) (_ret []inputAndKey[C], _err error) {
	if senderKey.VideoResolution == (codec.Resolution{}) && senderKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output resolution is not set (codec: %s)", senderKey.VideoCodec)
	}
	if senderKey.AudioSampleRate == 0 && senderKey.AudioCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output audio sample rate is not set (codec: %s)", senderKey.AudioCodec)
	}

	var inputsAndKeys []inputAndKey[C]
	switch s.MuxMode {
	case types.MuxModeDifferentOutputsSameTracks:
		inputsAndKeys = []inputAndKey[C]{{
			Input: &s.InputAll,
			Key:   senderKey,
		}}
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		inputsAndKeys = []inputAndKey[C]{}
		if senderKey.VideoCodec != "" {
			inputsAndKeys = append(inputsAndKeys, inputAndKey[C]{
				Input: s.InputVideoOnly,
				Key: SenderKey{
					VideoCodec:      senderKey.VideoCodec,
					VideoResolution: senderKey.VideoResolution,
				},
			})
		}
		if senderKey.AudioCodec != "" {
			inputsAndKeys = append(inputsAndKeys, inputAndKey[C]{
				Input: s.InputAudioOnly,
				Key: SenderKey{
					AudioCodec:      senderKey.AudioCodec,
					AudioSampleRate: senderKey.AudioSampleRate,
				},
			})
		}
	default:
		return nil, fmt.Errorf("unable to create and configure outputs in mux mode %s", s.MuxMode)
	}

	return inputsAndKeys, nil
}

func (s *StreamMux[C]) createAndConfigureOutputs(
	ctx context.Context,
	senderKey SenderKey,
	recoderConfig types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "createAndConfigureOutputs(ctx, %s)", senderKey)
	defer func() { logger.Tracef(ctx, "/createAndConfigureOutputs(ctx, %s): %v", senderKey, _err) }()

	inputsAndKeys, err := s.getInputsForSenderKey(ctx, senderKey)
	if err != nil {
		return fmt.Errorf("unable to get inputs for sender key %s: %w", senderKey, err)
	}

	for _, ik := range inputsAndKeys {
		input := ik.Input
		outputKey := ik.Key

		output, err := s.getOrCreateOutputLocked(ctx, outputKey, nil)
		if err != nil {
			return fmt.Errorf("unable to get-or-create the output %s: %w", outputKey, err)
		}

		logger.Debugf(ctx, "reconfiguring the output %d:%s", output.ID, outputKey)
		err = output.reconfigureRecoder(ctx, recoderConfig)
		if err != nil {
			return fmt.Errorf("unable to reconfigure the output %d:%s: %w", output.ID, outputKey, err)
		}

		logger.Debugf(ctx, "notifying about the sources")
		err = avpipeline.NotifyAboutPacketSources(ctx,
			input.Node.Processor.Kernel,
			output.InputFilter,
		)
		if err != nil {
			return fmt.Errorf("received an error while notifying nodes about packet sources (%s -> %s): %w", input.Node.Processor.Kernel, output.InputFilter, err)
		}
	}
	return nil
}

func (s *StreamMux[C]) GetAllStats(
	ctx context.Context,
) map[string]globaltypes.Statistics {
	return xsync.DoA1R1(ctx, &s.OutputsLocker, s.getAllStatsLocked, ctx)
}

func (s *StreamMux[C]) getAllStatsLocked(
	_ context.Context,
) map[string]globaltypes.Statistics {
	m := map[string]globaltypes.Statistics{}
	tryGetStats := func(key string, n node.Abstract) {
		m[key] = nodetypes.ToStatistics(n.GetCountersPtr(), n.GetProcessor().CountersPtr())
	}
	tryGetStats("Input", s.InputAll.Node)
	s.OutputsMap.Range(func(outputKey SenderKey, output *Output[C]) bool {
		tryGetStats(fmt.Sprintf("Output(%s):InputFilter", outputKey), output.InputFilter)
		tryGetStats(fmt.Sprintf("Output(%s):InputFixer", outputKey), output.InputFixer)
		tryGetStats(fmt.Sprintf("Output(%s):RecoderNode", outputKey), output.RecoderNode)
		tryGetStats(fmt.Sprintf("Output(%s):MapIndexes", outputKey), output.MapIndices)
		tryGetStats(fmt.Sprintf("Output(%s):OutputFixer", outputKey), output.SendingFixer)
		tryGetStats(fmt.Sprintf("Output(%s):OutputSyncFilter", outputKey), output.SendingSyncer)
		return true
	})
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

	logger.Debugf(ctx, "resulting graph: %s", node.Nodes[node.Abstract]{s.InputAll.Node}.StringRecursive())
	logger.Debugf(ctx, "resulting graph (graphviz): %s", node.Nodes[node.Abstract]{s.InputAll.Node}.DotString(false))

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
	return *xatomic.LoadPointer(&s.startedCh)
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

func (s *StreamMux[C]) WaitForActiveVideoOutput(
	ctx context.Context,
) *Output[C] {
	t := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			output := s.GetActiveVideoOutput(ctx)
			if output != nil {
				return output
			}
		}
	}
}

func (s *StreamMux[C]) waitForActiveVideoOutputLocked(
	ctx context.Context,
) *Output[C] {
	t := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			output := s.getActiveVideoOutputLocked(ctx)
			if output != nil {
				return output
			}
		}
	}
}

func (s *StreamMux[C]) GetActiveVideoOutput(
	ctx context.Context,
) *Output[C] {
	return xsync.DoA1R1(ctx, &s.OutputsLocker, s.getActiveVideoOutputLocked, ctx)
}

func (s *StreamMux[C]) getActiveVideoOutputLocked(
	ctx context.Context,
) *Output[C] {
	outputID := s.getVideoInput().OutputSwitch.CurrentValue.Load()
	logger.Tracef(ctx, "getActiveOutputLocked: outputID=%d", outputID)
	if int(outputID) < 0 || int(outputID) >= len(s.Outputs) {
		return nil
	}
	return s.Outputs[outputID]
}

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	return fmt.Sprintf("not implemented: %v", e.Err)
}

func (s *StreamMux[C]) setResolutionBitRateCodec(
	ctx context.Context,
	res codec.Resolution,
	bitRate types.Ubps,
	videoCodec codectypes.Name,
	audioCodec codectypes.Name,
) (_err error) {
	logger.Tracef(ctx, "setResolutionBitRateCodec: %v, %v, '%s', '%s'", res, bitRate, videoCodec, audioCodec)
	defer func() {
		logger.Tracef(ctx, "/setResolutionBitRateCodec: %v, %v, '%s', '%s': %v", res, bitRate, videoCodec, audioCodec, _err)
	}()
	return xsync.DoR1(ctx, &s.Locker, func() error {
		return s.setResolutionBitRateCodecLocked(ctx, res, bitRate, videoCodec, audioCodec)
	})
}

type ErrAlreadySet struct{}

func (e ErrAlreadySet) Error() string {
	return "already set"
}

func (s *StreamMux[C]) setResolutionBitRateCodecLocked(
	ctx context.Context,
	res codec.Resolution,
	bitRate types.Ubps,
	videoCodec codectypes.Name,
	audioCodec codectypes.Name,
) (_err error) {
	cfg := s.getCurrentOutputPropsLocked(ctx)
	if len(cfg.Output.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (have %d)", len(cfg.Output.AudioTrackConfigs))
	}
	audioCfg := cfg.Output.AudioTrackConfigs[0]

	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (have %d)", len(cfg.Output.VideoTrackConfigs))
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0]

	/*if strings.HasSuffix(string(videoCfg.CodecName), "_mediacodec") && res.Height < 720 {
		// TODO: this should not be here, it should be somewhere else.
		return ErrNotImplemented{Err: fmt.Errorf("when scaling from 1080p to let's say 480p, we get a distorted image when using mediacodec, to be investigated; until then this is forbidden")}
	}*/

	if videoCfg.Resolution == res && videoCfg.CodecName == videoCodec && audioCfg.CodecName == audioCodec {
		logger.Tracef(ctx, "the config is already set to %v '%s' '%s'", res, videoCodec, audioCodec)
		encoderV, _ := s.getVideoEncoderLocked(ctx)
		if videoCodec == codectypes.Name(codec.NameCopy) != codec.IsEncoderCopy(encoderV) {
			logger.Errorf(ctx, "the video codec is set to '%s', but the encoder is %s", videoCodec, encoderV)
		} else {
			videoCfg.AverageBitRate = uint64(bitRate)
			cfg.Output.VideoTrackConfigs[0] = videoCfg
			return ErrAlreadySet{}
		}
	}

	audioCfg.CodecName = audioCodec
	cfg.Output.AudioTrackConfigs[0] = audioCfg

	videoCfg.Resolution = res
	videoCfg.AverageBitRate = uint64(bitRate)
	videoCfg.CodecName = videoCodec
	cfg.Output.VideoTrackConfigs[0] = videoCfg

	err := s.switchToOutputByProps(ctx, types.SenderProps{
		RecoderConfig: cfg.RecoderConfig,
	}, false)
	if err != nil {
		return fmt.Errorf("unable to switch to the new output props: %w", err)
	}

	return nil
}

func (s *StreamMux[C]) SetFPSFraction(ctx context.Context, num, den uint32) {
	logger.Debugf(ctx, "SetFPSFraction: %d/%d", num, den)
	s.FPSFractionNumDen.Store((uint64(num) << 32) | uint64(den))
}

func (s *StreamMux[C]) GetFPSFraction(ctx context.Context) (num, den uint32) {
	numDen := s.FPSFractionNumDen.Load()
	num = uint32(numDen >> 32)
	den = uint32(numDen & 0xFFFFFFFF)
	return
}

func (s *StreamMux[C]) inputBitRateMeasurerLoop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "inputBitRateMeasurerLoop")
	defer func() { logger.Tracef(ctx, "/inputBitRateMeasurerLoop: %v", _err) }()

	t := time.NewTicker(time.Second / 4)
	defer t.Stop()
	var bytesVideoInputReadPrev uint64
	var bytesVideoEncodedGenPrev uint64
	var bytesVideoOutputReadPrev uint64
	var bytesAudioInputReadPrev uint64
	var bytesAudioEncodedGenPrev uint64
	var bytesAudioOutputReadPrev uint64
	var bytesOtherInputReadPrev uint64
	var bytesOtherEncodedGenPrev uint64
	var bytesOtherOutputReadPrev uint64
	tsPrev := time.Now()
	for {
		var tsNext time.Time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tsNext = <-t.C:
			duration := tsNext.Sub(tsPrev)

			inputCounters := s.InputAll.Node.GetCountersPtr()
			bytesVideoInputReadNext := inputCounters.Received.Packets.Video.Bytes.Load()
			bytesAudioInputReadNext := inputCounters.Received.Packets.Audio.Bytes.Load()
			bytesOtherInputReadNext := inputCounters.Received.Packets.Other.Bytes.Load()

			var bytesVideoEncodedGenNext uint64
			var bytesVideoOutputReadNext uint64
			var bytesAudioEncodedGenNext uint64
			var bytesAudioOutputReadNext uint64
			var bytesOtherEncodedGenNext uint64
			var bytesOtherOutputReadNext uint64
			s.OutputsMap.Range(func(outputKey SenderKey, output *Output[C]) bool {
				encoderCounters := output.RecoderNode.Processor.CountersPtr()
				bytesVideoEncodedGenNext += encoderCounters.Generated.Packets.Video.Bytes.Load() - encoderCounters.Omitted.Packets.Video.Bytes.Load()
				bytesAudioEncodedGenNext += encoderCounters.Generated.Packets.Audio.Bytes.Load() - encoderCounters.Omitted.Packets.Audio.Bytes.Load()
				bytesOtherEncodedGenNext += encoderCounters.Generated.Packets.Other.Bytes.Load() - encoderCounters.Omitted.Packets.Other.Bytes.Load()
				outputCounters := output.SendingNode.GetCountersPtr()
				bytesVideoOutputReadNext += outputCounters.Received.Packets.Video.Bytes.Load()
				bytesAudioOutputReadNext += outputCounters.Received.Packets.Audio.Bytes.Load()
				bytesOtherOutputReadNext += outputCounters.Received.Packets.Other.Bytes.Load()
				return true
			})

			bytesVideoInputRead := bytesVideoInputReadNext - bytesVideoInputReadPrev
			bitRateVideoInput := int(float64(bytesVideoInputRead*8) / duration.Seconds())
			bytesAudioInputRead := bytesAudioInputReadNext - bytesAudioInputReadPrev
			bitRateAudioInput := int(float64(bytesAudioInputRead*8) / duration.Seconds())
			bytesOtherInputRead := bytesOtherInputReadNext - bytesOtherInputReadPrev
			bitRateOtherInput := int(float64(bytesOtherInputRead*8) / duration.Seconds())

			oldVideoInputValue := s.CurrentVideoInputBitRate.Load()
			newVideoInputValue := updateWithInertialValue(oldVideoInputValue, uint64(bitRateVideoInput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			oldAudioInputValue := s.CurrentAudioInputBitRate.Load()
			newAudioInputValue := updateWithInertialValue(oldAudioInputValue, uint64(bitRateAudioInput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			oldOtherInputValue := s.CurrentOtherInputBitRate.Load()
			newOtherInputValue := updateWithInertialValue(oldOtherInputValue, uint64(bitRateOtherInput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			logger.Debugf(ctx, "input bitrate: %d | %d | %d: %s | %s | %s: (%d-%d)*8/%v | (%d-%d)*8/%v | (%d-%d)*8/%v; setting %d:%d:%d as the new value",
				bitRateVideoInput, bitRateAudioInput, bitRateOtherInput,
				humanize.SI(float64(bitRateVideoInput), "bps"), humanize.SI(float64(bitRateAudioInput), "bps"), humanize.SI(float64(bitRateOtherInput), "bps"),
				bytesVideoInputReadNext, bytesVideoInputReadPrev, duration,
				bytesAudioInputReadNext, bytesAudioInputReadPrev, duration,
				bytesOtherInputReadNext, bytesOtherInputReadPrev, duration,
				newVideoInputValue, newAudioInputValue, newOtherInputValue,
			)
			s.CurrentVideoInputBitRate.Store(newVideoInputValue)
			s.CurrentAudioInputBitRate.Store(newAudioInputValue)
			s.CurrentOtherInputBitRate.Store(newOtherInputValue)

			bytesVideoEncodedGen := bytesVideoEncodedGenNext - bytesVideoEncodedGenPrev
			bytesAudioEncodedGen := bytesAudioEncodedGenNext - bytesAudioEncodedGenPrev
			bytesOtherEncodedGen := bytesOtherEncodedGenNext - bytesOtherEncodedGenPrev
			bitRateVideoEncoded := int(float64(bytesVideoEncodedGen*8) / duration.Seconds())
			bitRateAudioEncoded := int(float64(bytesAudioEncodedGen*8) / duration.Seconds())
			bitRateOtherEncoded := int(float64(bytesOtherEncodedGen*8) / duration.Seconds())

			oldVideoEncodedValue := s.CurrentVideoEncodedBitRate.Load()
			newVideoEncodedValue := updateWithInertialValue(oldVideoEncodedValue, uint64(bitRateVideoEncoded), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			oldAudioEncodedValue := s.CurrentAudioEncodedBitRate.Load()
			newAudioEncodedValue := updateWithInertialValue(oldAudioEncodedValue, uint64(bitRateAudioEncoded), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			oldOtherEncodedValue := s.CurrentOtherEncodedBitRate.Load()
			newOtherEncodedValue := updateWithInertialValue(oldOtherEncodedValue, uint64(bitRateOtherEncoded), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			logger.Debugf(ctx, "encoded bitrate: %d | %d | %d: %s | %s | %s: (%d-%d)*8/%v | (%d-%d)*8/%v | (%d-%d)*8/%v; setting %d:%d:%d as the new value",
				bitRateVideoEncoded, bitRateAudioEncoded, bitRateOtherEncoded,
				humanize.SI(float64(bitRateVideoEncoded), "bps"), humanize.SI(float64(bitRateAudioEncoded), "bps"), humanize.SI(float64(bitRateOtherEncoded), "bps"),
				bytesVideoEncodedGenNext, bytesVideoEncodedGenPrev, duration,
				bytesAudioEncodedGenNext, bytesAudioEncodedGenPrev, duration,
				bytesOtherEncodedGenNext, bytesOtherEncodedGenPrev, duration,
				newVideoEncodedValue, newAudioEncodedValue, newOtherEncodedValue,
			)
			s.CurrentVideoEncodedBitRate.Store(newVideoEncodedValue)
			s.CurrentAudioEncodedBitRate.Store(newAudioEncodedValue)
			s.CurrentOtherEncodedBitRate.Store(newOtherEncodedValue)

			bytesVideoOutputRead := bytesVideoOutputReadNext - bytesVideoOutputReadPrev
			bitRateVideoOutput := int(float64(bytesVideoOutputRead*8) / duration.Seconds())
			bytesAudioOutputRead := bytesAudioOutputReadNext - bytesAudioOutputReadPrev
			bitRateAudioOutput := int(float64(bytesAudioOutputRead*8) / duration.Seconds())
			bytesOtherOutputRead := bytesOtherOutputReadNext - bytesOtherOutputReadPrev
			bitRateOtherOutput := int(float64(bytesOtherOutputRead*8) / duration.Seconds())

			oldVideoOutputValue := s.CurrentVideoOutputBitRate.Load()
			newVideoOutputValue := updateWithInertialValue(oldVideoOutputValue, uint64(bitRateVideoOutput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			oldAudioOutputValue := s.CurrentAudioOutputBitRate.Load()
			newAudioOutputValue := updateWithInertialValue(oldAudioOutputValue, uint64(bitRateAudioOutput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			oldOtherOutputValue := s.CurrentOtherOutputBitRate.Load()
			newOtherOutputValue := updateWithInertialValue(oldOtherOutputValue, uint64(bitRateOtherOutput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			logger.Debugf(ctx, "output bitrate: %d | %d | %d: %s | %s | %s: (%d-%d)*8/%v | (%d-%d)*8/%v | (%d-%d)*8/%v; setting %d:%d:%d as the new value",
				bitRateVideoOutput, bitRateAudioOutput, bitRateOtherOutput,
				humanize.SI(float64(bitRateVideoOutput), "bps"), humanize.SI(float64(bitRateAudioOutput), "bps"), humanize.SI(float64(bitRateOtherOutput), "bps"),
				bytesVideoOutputReadNext, bytesVideoOutputReadPrev, duration,
				bytesAudioOutputReadNext, bytesAudioOutputReadPrev, duration,
				bytesOtherOutputReadNext, bytesOtherOutputReadPrev, duration,
				newVideoOutputValue, newAudioOutputValue, newOtherOutputValue,
			)
			s.CurrentVideoOutputBitRate.Store(newVideoOutputValue)
			s.CurrentAudioOutputBitRate.Store(newAudioOutputValue)
			s.CurrentOtherOutputBitRate.Store(newOtherOutputValue)

			// DONE

			s.CurrentBitRateMeasurementsCount.Add(1)

			bytesVideoInputReadPrev = bytesVideoInputReadNext
			bytesVideoEncodedGenPrev = bytesVideoEncodedGenNext
			bytesVideoOutputReadPrev = bytesVideoOutputReadNext
			bytesAudioInputReadPrev = bytesAudioInputReadNext
			bytesAudioEncodedGenPrev = bytesAudioEncodedGenNext
			bytesAudioOutputReadPrev = bytesAudioOutputReadNext
			bytesOtherInputReadPrev = bytesOtherInputReadNext
			bytesOtherEncodedGenPrev = bytesOtherEncodedGenNext
			bytesOtherOutputReadPrev = bytesOtherOutputReadNext
			tsPrev = tsNext
		}
	}
}

func updateWithInertialValue(
	oldValue, newValue uint64,
	inertia float64,
	measurementsCount uint64,
) uint64 {
	// make it volatile in the beginning, and more stable later:
	effectiveInertia := inertia * (float64(measurementsCount) / float64(measurementsCount+3))
	return uint64(float64(oldValue)*effectiveInertia + float64(newValue)*(1-effectiveInertia))
}

func (s *StreamMux[C]) GetBestNotBypassOutput(
	ctx context.Context,
) (_ret *Output[C]) {
	logger.Tracef(ctx, "GetBestNotBypassOutput")
	defer func() { logger.Tracef(ctx, "/GetBestNotBypassOutput: %v", _ret) }()
	return xsync.DoA1R1(ctx, &s.OutputsLocker, s.getBestNotBypassOutputLocked, ctx)
}

func (s *StreamMux[C]) getBestNotBypassOutputLocked(
	ctx context.Context,
) (_ret *Output[C]) {
	var outputKeys SenderKeys
	s.OutputsMap.Range(func(outputKey SenderKey, _ *Output[C]) bool {
		outputKeys = append(outputKeys, outputKey)
		return true
	})
	outputKeys.Sort()
	for _, outputKey := range outputKeys {
		if outputKey.VideoCodec == codectypes.Name(codec.NameCopy) {
			continue
		}
		output, _ := s.OutputsMap.Load(outputKey)
		return output
	}
	return nil
}

func (s *StreamMux[C]) GetEncoders(
	ctx context.Context,
) (codec.Encoder, codec.Encoder) {
	return xsync.DoA1R2(ctx, &s.Locker, s.getVideoEncoderLocked, ctx)
}

func (s *StreamMux[C]) getVideoEncoderLocked(
	ctx context.Context,
) (codec.Encoder, codec.Encoder) {
	o := s.getActiveVideoOutputLocked(ctx)
	if o == nil {
		return nil, nil
	}

	var (
		vEnc codec.Encoder
		aEnc codec.Encoder
	)

	vEncoders := o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.VideoEncoders
	if len(vEncoders) == 1 {
		vEnc = vEncoders[0]
	}

	aEncoders := o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.AudioEncoders
	if len(aEncoders) == 1 {
		aEnc = aEncoders[0]
	}

	return vEnc, aEnc
}

func (s *StreamMux[C]) onInputPacket(
	ctx context.Context,
	pkt *packet.Input,
) (_err error) {
	logger.Tracef(ctx, "onInputPacket: %v", pkt.GetPTS())
	defer func() { logger.Tracef(ctx, "/onInputPacket: %v: %v", pkt.GetPTS(), _err) }()

	if pkt.GetMediaType() != astiav.MediaTypeVideo {
		return nil
	}

	isKey := pkt.Flags().Has(astiav.PacketFlagKey)
	if !isKey {
		return nil
	}

	streamIndex := pkt.StreamIndex()

	s.Locker.Do(ctx, func() {
		if s.lastKeyFrames[streamIndex] == nil {
			s.lastKeyFrames[streamIndex] = ringbuffer.New[packet.Input](2)
		}
		s.lastKeyFrames[streamIndex].Add(packet.BuildInput(
			packet.CloneAsReferenced(pkt.Packet),
			pkt.StreamInfo,
		))
	})
	return nil
}

func (s *StreamMux[C]) InitOutputVideoStreams(
	ctx context.Context,
	receiver node.Abstract,
	outputKey SenderKey,
) (_err error) {
	isBypass := outputKey.VideoCodec == codectypes.Name(codec.NameCopy)
	logger.Debugf(ctx, "InitOutputStreams: isBypass:%v", isBypass)
	defer func() { logger.Debugf(ctx, "/InitOutputStreams: isBypass:%v: %v", isBypass, _err) }()
	if !isBypass {
		return nil
	}
	return xsync.DoA2R1(ctx, &s.Locker, s.initOutputStreamsLocked, ctx, receiver)
}

func (s *StreamMux[C]) initOutputStreamsLocked(
	ctx context.Context,
	receiver node.Abstract,
) (_err error) {
	logger.Tracef(ctx, "initOutputStreamsLocked")
	defer func() { logger.Tracef(ctx, "/initOutputStreamsLocked: %v", _err) }()
	counters := receiver.GetCountersPtr()
	inputCh := receiver.GetProcessor().InputPacketChan()
	var streamIndices []int
	for streamIndex := range s.lastKeyFrames {
		streamIndices = append(streamIndices, streamIndex)
	}
	slices.Sort(streamIndices)
	for _, streamIndex := range streamIndices {
		buf := s.lastKeyFrames[streamIndex]
		items := buf.GetAll()
		logger.Debugf(ctx, "initOutputStreamsLocked: re-sending %d last keyframes for stream index %d", len(items), streamIndex)
		for _, pkt := range items {
			mediaType := globaltypes.MediaType(pkt.GetMediaType())
			dataSize := uint64(len(pkt.Data()))
			counters.Addressed.Packets.Get(mediaType).Increment(dataSize)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case inputCh <- pkt:
			}
			counters.Received.Packets.Get(mediaType).Increment(dataSize)
		}
	}
	return nil
}

func (s *StreamMux[C]) GetBitRates(
	ctx context.Context,
) (_ret *types.BitRates, _err error) {
	logger.Tracef(ctx, "GetBitRates")
	defer func() { logger.Tracef(ctx, "/GetBitRates: %v, %v", _ret, _err) }()

	result := &types.BitRates{
		Input: types.BitRateInfo{
			Video: types.Ubps(s.CurrentVideoInputBitRate.Load()),
			Audio: types.Ubps(s.CurrentAudioInputBitRate.Load()),
			Other: types.Ubps(s.CurrentOtherInputBitRate.Load()),
		},
		Encoded: types.BitRateInfo{
			Video: types.Ubps(s.CurrentVideoEncodedBitRate.Load()),
			Audio: types.Ubps(s.CurrentAudioEncodedBitRate.Load()),
			Other: types.Ubps(s.CurrentOtherEncodedBitRate.Load()),
		},
		Output: types.BitRateInfo{
			Video: types.Ubps(s.CurrentVideoOutputBitRate.Load()),
			Audio: types.Ubps(s.CurrentAudioOutputBitRate.Load()),
			Other: types.Ubps(s.CurrentOtherOutputBitRate.Load()),
		},
	}

	return result, nil
}
