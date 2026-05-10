// stream_mux.go implements a handler that routes audio and video streams to different outputs.

// Package streammux provides a handler that routes audio and video streams to different outputs.
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
	"github.com/facebookincubator/go-belt"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	nodeboilerplate "github.com/xaionaro-go/avpipeline/node/boilerplate"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/fanout"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
	"tailscale.com/util/ringbuffer"
)

const (
	switchTimeout = time.Hour

	streamMuxRouteAll       id.RouteID = "all"
	streamMuxRouteAudioOnly id.RouteID = "audio-only"
	streamMuxRouteVideoOnly id.RouteID = "video-only"
)

var EnableDraining = false // TODO: enable this (currently it causes bugs with mediacodec)

type StreamMux[C any] struct {
	MuxMode            types.MuxMode
	CurrentOutputProps types.SenderProps
	Locker             xsync.Mutex

	// QuietOnMissingEncoder demotes the by-design "unable to get
	// encoder" log spam (emitted by AutoBitRateHandler when no input
	// is flowing yet, so no encoder is bound) from Warn to Debug. It
	// is set externally (e.g. by ffstream's -quiet_on_open_failure
	// CLI flag, also available as the legacy alias
	// -quiet_empty_priority). Default (false) preserves the legacy
	// Warnf so existing diagnostics aren't lost.
	QuietOnMissingEncoder atomic.Bool

	// RawFrameSource records that the upstream pipeline supplies
	// decoded frames directly (e.g. android_camera + android_microphone)
	// rather than packets that need a downstream decoder. When set,
	// every Output created via getOrCreateOutputLocked is initialised
	// with OptionRawFrameSource(true), which routes MediaCodec
	// encoders away from the get_format -> AV_PIX_FMT_MEDIACODEC
	// surface-passthrough trap. Default (false) keeps the legacy
	// behaviour for transcoding-from-decoder pipelines.
	//
	// OneWayBool pins the sticky-true contract at the type level: the
	// flag can only ever transition false→true, never back. The first
	// raw-frame upstream observed at any point in the StreamMux life-
	// cycle latches it, and the latched encoder pix_fmt stays valid
	// even if the raw-frame source is later removed.
	RawFrameSource globaltypes.OneWayBool

	// inputs:
	InputAll       Input[C]
	InputAudioOnly *Input[C]
	InputVideoOnly *Input[C]

	// outputs:
	Outputs       xsync.Map[OutputID, *Output[C]]
	OutputsMap    xsync.Map[SenderKey, *Output[C]]
	OutputsLocker xsync.Mutex
	SenderFactory SenderFactory[C]

	// aux
	AutoBitRateHandler      *AutoBitRateHandler[C]
	FPSFractionNumDen       atomic.Uint64
	VideoOutputBeforeBypass *Output[C]

	// measurements
	Measurements                    map[astiav.MediaType]*TrackMeasurements
	CurrentBitRateMeasurementsCount atomic.Uint64
	LastLatencyCheckTS              atomic.Uint64

	// to become a fake node myself:
	nodeboilerplate.Counters
	nodeboilerplate.InputFilter

	// private:
	startedCh           *chan struct{}
	waitGroup           sync.WaitGroup
	lastKeyFrames       map[int]*ringbuffer.RingBuffer[packetorframe.InputUnion]
	nextOutputID        atomic.Uint32
	allowCorruptPackets atomic.Bool

	// lastEvictedKey records the SenderKey of the most-recently-
	// evicted output for each Input. The 1 Hz retry tick reads this
	// to know which key to recreate when an Input is still orphaned
	// (OutputSwitch.CurrentValue == math.MinInt32). See
	// RETRY_SEMANTICS.md for the design rationale.
	lastEvictedKey xsync.Map[*Input[C], SenderKey]

	// retryTracker owns the selector-level orphan retry state. lastEvictedKey
	// remains as the streammux compatibility mirror used by existing tests
	// and diagnostics.
	retryTracker *orphanretry.Tracker[SenderKey]

	// recreateEvictedOutputFunc is the test seam for the no-sibling
	// recreate path. Production wires it to recreateEvictedOutputDefault
	// in NewWithCustomData; tests inject a stub to assert call timing
	// without spinning up a real Transcoder/Encoder factory chain.
	recreateEvictedOutputFunc func(ctx context.Context, input *Input[C], deadOutputKey SenderKey) error

	// evictDemoteTestHook is a test-only seam fired inside evictDeadOutput
	// IMMEDIATELY AFTER OutputSwitch.CurrentValue is demoted to MinInt32
	// for an input. nil in production. Used to deterministically observe
	// the post-demote intermediate state for the Store-before-demote
	// ordering invariant — see TestEvictionRecreate_StoreBeforeDemote.
	evictDemoteTestHook func(input *Input[C])
}

func New(
	ctx context.Context,
	muxMode types.MuxMode,
	senderFactory SenderFactory[struct{}],
) (*StreamMux[struct{}], error) {
	return NewWithCustomData(
		ctx,
		muxMode,
		senderFactory,
	)
}

func NewWithCustomData[C any](
	ctx context.Context,
	muxMode types.MuxMode,
	senderFactory SenderFactory[C],
) (*StreamMux[C], error) {
	s := &StreamMux[C]{
		SenderFactory: senderFactory,
		MuxMode:       muxMode,

		lastKeyFrames: map[int]*ringbuffer.RingBuffer[packetorframe.InputUnion]{},
		startedCh:     ptr(make(chan struct{})),
		Measurements: map[astiav.MediaType]*TrackMeasurements{
			astiav.MediaTypeVideo:    newTrackMeasurements(),
			astiav.MediaTypeAudio:    newTrackMeasurements(),
			astiav.MediaTypeSubtitle: newTrackMeasurements(),
			astiav.MediaTypeData:     newTrackMeasurements(),
			astiav.MediaTypeUnknown:  newTrackMeasurements(),
		},
	}
	// Wire the default eviction-recovery recreator. Exposed as a struct
	// field (not a hard-coded call) so tests can intercept the call to
	// assert backoff behaviour without having to spin up a real
	// Transcoder/Encoder factory chain — the recreator is the only side-
	// effect the no-sibling branch exposes.
	s.recreateEvictedOutputFunc = s.recreateEvictedOutputDefault
	retryTracker, err := orphanretry.NewTracker[SenderKey](
		orphanretry.StreamMuxCompatibilityPolicy[SenderKey](),
		time.Now,
		s.recreateEvictedOutputForRoute,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize eviction retry tracker: %w", err)
	}
	s.retryTracker = retryTracker
	inputAll, err := newInput(ctx, s, InputTypeAll)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize all input: %w", err)
	}
	s.InputAll = *inputAll
	if muxMode == types.MuxModeDifferentOutputsSameTracksSplitAV {
		s.InputAudioOnly, err = newInput(ctx, s, InputTypeAudioOnly)
		if err != nil {
			return nil, fmt.Errorf("unable to initialize audio-only input: %w", err)
		}
		s.InputVideoOnly, err = newInput(ctx, s, InputTypeVideoOnly)
		if err != nil {
			return nil, fmt.Errorf("unable to initialize video-only input: %w", err)
		}
		s.InputAll.Node.AddPushTo(ctx, s.InputAudioOnly.Node, packetorframefiltercondition.Or{
			packetorframefiltercondition.MediaType(astiav.MediaTypeAudio),
			packetorframefiltercondition.MediaType(astiav.MediaTypeSubtitle),
			packetorframefiltercondition.MediaType(astiav.MediaTypeData),
		})
		s.InputAll.Node.AddPushTo(ctx, s.InputVideoOnly.Node, packetorframefiltercondition.MediaType(astiav.MediaTypeVideo))
		s.allowCorruptPackets.Store(true) // to initialize stream on the remote side quickly, we allow blank frames (this is applicable not only to MuxModeDifferentOutputsSameTracksSplitAV, but we tested it only here for now)
	}
	s.getTrackMeasurements(astiav.MediaTypeAudio).InputBitRate.Store(192_000) // some reasonable high end guess
	s.getTrackMeasurements(astiav.MediaTypeUnknown).InputBitRate.Store(0)
	if err := s.initSwitches(ctx); err != nil {
		return nil, fmt.Errorf("unable to initialize switches: %w", err)
	}
	s.getTrackMeasurements(astiav.MediaTypeVideo).InputBitRate.Store(20_000_000) // some reasonable high end guess
	return s, nil
}

func (s *StreamMux[C]) GetAutoBitRateHandler() *AutoBitRateHandler[C] {
	return xatomic.LoadPointer(&s.AutoBitRateHandler)
}

func (s *StreamMux[C]) swapAutoBitRateHandler(
	new *AutoBitRateHandler[C],
	old *AutoBitRateHandler[C],
) bool {
	return xatomic.CompareAndSwapPointer(&s.AutoBitRateHandler, old, new)
}

// swapAndCloseAutoBitRateHandler atomically swaps `new` into the
// auto-bitrate slot replacing `old`, then Close()s `old` if non-nil.
// On CAS failure it returns an error and does NOT close anything: the
// caller owns the surplus `new` (which was constructed but never
// installed) and must Close it itself, while `old` remains live under
// the concurrent owner that won the race.
//
// The Close on the replaced handler must run BEFORE any subsequent
// h.start(ctx) on `new`: otherwise both the old and new handler
// goroutines tick concurrently for one CheckInterval and race on the
// encoder bitrate (the bug commit 54954aa fixed). Between the CAS and
// the start the auto-bitrate slot reads either `new` (post-swap) or
// nil (post-clear), never two parallel writers.
func (s *StreamMux[C]) swapAndCloseAutoBitRateHandler(
	ctx context.Context,
	new *AutoBitRateHandler[C],
	old *AutoBitRateHandler[C],
) error {
	if !s.swapAutoBitRateHandler(new, old) {
		return fmt.Errorf("unable to set auto bitrate handler, concurrent modification detected")
	}
	if old != nil {
		if err := old.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close previous auto bitrate handler: %v", err)
		}
	}
	return nil
}

func (s *StreamMux[C]) SetAutoBitRateVideoConfig(
	ctx context.Context,
	autoBitRate *AutoBitRateVideoConfig,
) (_err error) {
	logger.Debugf(ctx, "SetAutoBitRateVideoConfig(%#+v)", autoBitRate)
	defer func() { logger.Debugf(ctx, "/SetAutoBitRateVideoConfig(%#+v): %v", autoBitRate, _err) }()

	oldAutoBitRate := s.GetAutoBitRateHandler()
	if autoBitRate == nil {
		if oldAutoBitRate == nil {
			logger.Debugf(ctx, "automatic bitrate control is already disabled")
			return nil
		}

		logger.Debugf(ctx, "disabling automatic bitrate control")
		err := s.removeAutoBitRateHandler(ctx)
		if err != nil {
			return fmt.Errorf("unable to stop auto bitrate handler: %w", err)
		}
		return nil
	}

	logger.Debugf(ctx, "enabling automatic bitrate control")
	h, err := s.newAutoBitRateHandler(ctx, *autoBitRate)
	if err != nil {
		return fmt.Errorf("unable to initialize auto bitrate handler: %w", err)
	}
	if err := s.swapAndCloseAutoBitRateHandler(ctx, h, oldAutoBitRate); err != nil {
		// CAS lost the race: `h` was constructed but never installed,
		// so we own it and must Close it. `oldAutoBitRate` was not
		// closed by the helper and remains under the concurrent owner.
		if closeErr := h.Close(ctx); closeErr != nil {
			logger.Errorf(ctx, "unable to close superfluous auto bitrate handler: %v", closeErr)
		}
		return err
	}
	if err := h.start(ctx); err != nil {
		return fmt.Errorf("unable to start auto bitrate handler: %w", err)
	}
	return nil
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
		if input == nil {
			continue
		}
		err := fn(ctx, input)
		switch err {
		case nil:
		case ErrStop{}:
			return nil
		default:
			return fmt.Errorf("unable to process input %s: %w", input.GetType(), err)
		}
	}
	return nil
}

func (s *StreamMux[C]) GetVideoInput(
	ctx context.Context,
) *Input[C] {
	if s.InputVideoOnly != nil {
		return s.InputVideoOnly
	}
	return &s.InputAll
}

func (s *StreamMux[C]) GetVideoOutputIDSwitchingTo(
	ctx context.Context,
) (_ret *OutputID) {
	logger.Tracef(ctx, "GetVideoOutputIDSwitchingTo()")
	defer func() { logger.Tracef(ctx, "/GetVideoOutputIDSwitchingTo(): %v", _ret) }()
	input := s.GetVideoInput(ctx)
	outputID := input.OutputSwitch.NextValue.Load()
	if outputID < 0 {
		return nil
	}
	return ptr(OutputID(outputID))
}

func (s *StreamMux[C]) initSwitches(
	ctx context.Context,
) error {
	return s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		inputType := input.GetType()

		input.OutputSwitch.CurrentValue.Store(math.MinInt32)
		input.OutputSyncer.CurrentValue.Store(math.MinInt32)

		if inputType.IncludesMediaType(astiav.MediaTypeVideo) {
			keepUnlessConds := packetorframecondition.And{
				packetorframecondition.MediaType(astiav.MediaTypeVideo),
				packetorframecondition.Or{
					packetorframecondition.IsKeyFrame(true),
					packetorframecondition.AtomicBool(&s.allowCorruptPackets),
					// Intra-only codecs (rawvideo, wrapped_avframe) carry no
					// inter-frame prediction so every packet is effectively
					// a keyframe even when libav demuxers (e.g. android_camera
					// emitting rawvideo) do not set AV_PKT_FLAG_KEY. Without
					// this the OutputSwitch keep-unless never matches a
					// switch-anchor packet — the cam frames flow but the
					// barrier never commits to the new output, so the
					// per-Output TranscoderNode is never reached, the
					// EncoderFactory.VideoEncoders slice stays empty, and
					// AutoBitRateHandler's checkOnce loops forever logging
					// "unable to get encoder". The codec list itself is the
					// single source of truth in codec/intra_only.go. Mirror it
					// here through packetorframecondition.IsIntraOnlyCodec
					// rather than duplicated case-by-case so the streammux,
					// inputwithfallback InputSwitch, and inputwithfallback
					// Syncer keep-unless lists cannot silently diverge.
					packetorframecondition.IsIntraOnlyCodec{},
				},
			}
			logger.Debugf(ctx, "Switch[%s]: setting keep-unless conditions: %s", inputType, keepUnlessConds)
			input.OutputSwitch.SetKeepUnless(keepUnlessConds)
		}

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

		input.OutputSwitch.SetOnAfterSwitch(func(
			ctx context.Context,
			in packetorframe.InputUnion,
			from, to int32,
		) {
			if v := in.Get(); v != nil {
				ctx = belt.WithField(ctx, "media_type", v.GetMediaType().String())
			}
			logger.Debugf(ctx, "Switch[%s].SetOnAfterSwitch: %d -> %d", inputType, from, to)

			logger.Debugf(ctx, "Syncer[%s].SetValue(ctx, %d): from %d", inputType, to, from)
			err := input.OutputSyncer.SetValue(ctx, to)
			logger.Debugf(ctx, "/Syncer[%s].SetValue(ctx, %d): from %d: %v", inputType, to, from, err)

			s.OutputsLocker.ManualLock(ctx)
			outputNext, _ := s.Outputs.Load(OutputID(to))
			if outputNext == nil {
				logger.Errorf(ctx, "Switch[%s]: next output %d not found", inputType, to)
				s.OutputsLocker.ManualUnlock(ctx)
				input.OutputSyncer.SetValue(ctx, from)
				return
			}
			if h := s.GetAutoBitRateHandler(); h != nil {
				h.onOutputSwitch(ctx, inputType, from, to)
			}
			if err := outputNext.SetForceNextFrameKey(ctx, true); err != nil {
				logger.Errorf(ctx, "Switch[%s]: unable to set force key frame on the output %d: %v", inputType, to, err)
			}
			observability.Go(ctx, func(ctx context.Context) {
				defer s.OutputsLocker.ManualUnlock(ctx)
				if from == math.MinInt32 {
					return
				}
				outputPrev, _ := s.Outputs.Load(OutputID(from))
				if outputPrev == nil {
					logger.Errorf(ctx, "Switch[%s]: previous output %d not found", inputType, from)
					return
				}

				input := outputPrev.Input()
				if err := outputPrev.InputFrom.RemovePushTo(ctx, input); err != nil {
					logger.Errorf(ctx, "Switch[%s]: unable to remove push to the output %d: %v", inputType, from, err)
				}

				if EnableDraining {
					ctx, cancelFn := context.WithTimeout(ctx, switchTimeout)
					defer cancelFn()
					err := outputPrev.Drain(ctx)
					if err != nil {
						logger.Errorf(ctx, "Switch[%s]: unable to close the output %d: %v", inputType, from, err)
					}
				}

				outputPrev.FirstNodeAfterFilter().SetInputFilter(ctx, packetorframefiltercondition.Panic("Switch["+inputType.String()+"]: somehow received a packet, while the output is closed"))

				observability.Go(ctx, func(ctx context.Context) {
					err := outputPrev.CloseNoDrain(ctx)
					if err != nil {
						logger.Errorf(ctx, "Switch[%s]: unable to close the output %d: %v", inputType, from, err)
					}
				})
				observability.Go(ctx, func(ctx context.Context) {
					s.Locker.Do(ctx, func() {
						err := s.createAndConfigureOutputs(ctx, outputPrev.GetKey(), s.CurrentOutputProps.TranscoderConfig)
						if err != nil {
							logger.Errorf(ctx, "Switch[%s]: unable to re-initialize the output %d:%s: %v", inputType, outputPrev.ID, outputPrev.GetKey(), err)
						}
					})
				})
			})
		})

		// To prevent corrupting the stream by mixing packets via two different Output:
		//
		// Different output exists to allow different codecs/resolutions/whatnot, but
		// they are not really different outputs, they all lead to the same destination.
		// Thus the order of the packets is important. Thus we first allow the previous,
		// output to empty, while holding packets in the new output (thus InactiveBlock),
		// and after that release the held packets.
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
	output, ok := s.Outputs.Load(OutputID(outputID))
	if !ok || output == nil {
		logger.Errorf(ctx, "removeOutputByIDLocked: output %v not found in Outputs map", outputID)
		return fmt.Errorf("output %v not found", outputID)
	}
	// StorageKey() (not GetKey()) — see Output.StorageKey godoc.
	// removeOutputLocked feeds OutputsMap.LoadAndDelete, which must use
	// the key the entry was stored under, not the live
	// EncoderFactory-derived compound key.
	return s.removeOutputLocked(ctx, output.StorageKey())
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
	cmp, ok := s.Outputs.LoadAndDelete(OutputID(output.ID))
	assert(ctx, ok && cmp == output, "Outputs and OutputsMap are out of sync")
	return nil
}

var _ = (*StreamMux[struct{}])(nil).removeOutputLocked

// ResetOutputs removes every currently configured output and demotes
// input routing state so a subsequent switch creates fresh output chains.
func (s *StreamMux[C]) ResetOutputs(
	ctx context.Context,
) error {
	return xsync.DoA1R1(ctx, &s.Locker, s.ResetOutputsLocked, ctx)
}

// ResetOutputsLocked is ResetOutputs for callers already holding
// StreamMux.Locker while coordinating output configuration changes.
func (s *StreamMux[C]) ResetOutputsLocked(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "ResetOutputsLocked")
	defer func() { logger.Debugf(ctx, "/ResetOutputsLocked: %v", _err) }()

	var outputs []*Output[C]
	seenOutputs := map[*Output[C]]struct{}{}
	s.OutputsLocker.Do(ctx, func() {
		s.OutputsMap.Range(func(outputKey SenderKey, output *Output[C]) bool {
			s.OutputsMap.Delete(outputKey)
			if output == nil {
				return true
			}
			s.Outputs.Delete(output.ID)
			if _, ok := seenOutputs[output]; ok {
				return true
			}
			seenOutputs[output] = struct{}{}
			outputs = append(outputs, output)
			return true
		})
		s.Outputs.Range(func(outputID OutputID, output *Output[C]) bool {
			s.Outputs.Delete(outputID)
			if output == nil {
				return true
			}
			if _, ok := seenOutputs[output]; ok {
				return true
			}
			seenOutputs[output] = struct{}{}
			outputs = append(outputs, output)
			return true
		})
	})

	var errs []error
	for _, output := range outputs {
		s.detachOutputInputFromPushGraph(ctx, output)
		if err := s.demoteOutputReferences(ctx, output); err != nil {
			errs = append(errs, fmt.Errorf("unable to demote output %d references: %w", output.ID, err))
		}
		if err := output.CloseNoDrain(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close output %d: %w", output.ID, err))
		}
	}
	if err := s.clearOutputRecoveryState(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to clear output recovery state: %w", err))
	}
	return errors.Join(errs...)
}

func (s *StreamMux[C]) demoteOutputReferences(
	ctx context.Context,
	output *Output[C],
) error {
	return s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		demotion := input.outputPair.DemoteIfCurrent(ctx, id.MemberID(output.ID))
		if demotion.SwitchDemoted {
			logger.Debugf(ctx, "demoted OutputSwitch.CurrentValue from %d to MinInt32 on input %s", output.ID, input.GetType())
		}
		if demotion.SyncerDemoted {
			logger.Debugf(ctx, "demoted OutputSyncer.CurrentValue from %d to MinInt32 on input %s", output.ID, input.GetType())
		}
		input.OutputSwitch.NextValue.CompareAndSwap(int32(output.ID), math.MinInt32)
		input.OutputSyncer.NextValue.CompareAndSwap(int32(output.ID), math.MinInt32)
		return nil
	})
}

func (s *StreamMux[C]) clearOutputRecoveryState(
	ctx context.Context,
) error {
	return s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		s.lastEvictedKey.Delete(input)
		if s.retryTracker == nil {
			return nil
		}
		routeID, err := s.routeIDForInput(input)
		if err != nil {
			return err
		}
		s.retryTracker.MarkRecovered(ctx, routeID)
		return nil
	})
}

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
	if h := s.GetAutoBitRateHandler(); h != nil {
		if err := h.Close(ctx); err != nil {
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

func fanOutModeForMuxMode(
	muxMode types.MuxMode,
) (fanout.Mode, error) {
	switch muxMode {
	case types.MuxModeForbid:
		return fanout.ModeForbid, nil
	case types.MuxModeSameOutputSameTracks:
		return fanout.ModeSameOutputSameTracks, nil
	case types.MuxModeSameOutputDifferentTracks:
		return fanout.ModeSameOutputDifferentTracks, nil
	case types.MuxModeDifferentOutputsSameTracks:
		return fanout.ModeDifferentOutputsSameTracks, nil
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		return fanout.ModeDifferentOutputsSameTracksSplitAV, nil
	case types.UndefinedMuxMode:
		return 0, fmt.Errorf("mux mode is not defined")
	default:
		return 0, fmt.Errorf("unknown mux mode: %s", muxMode)
	}
}

func routeIDForInputType(
	inputType InputType,
) (id.RouteID, error) {
	switch inputType {
	case InputTypeAll:
		return streamMuxRouteAll, nil
	case InputTypeAudioOnly:
		return streamMuxRouteAudioOnly, nil
	case InputTypeVideoOnly:
		return streamMuxRouteVideoOnly, nil
	default:
		return "", fmt.Errorf("unknown input type: %s", inputType)
	}
}

func inputTypeForRouteID(
	routeID id.RouteID,
) (InputType, bool) {
	switch routeID {
	case streamMuxRouteAll:
		return InputTypeAll, true
	case streamMuxRouteAudioOnly:
		return InputTypeAudioOnly, true
	case streamMuxRouteVideoOnly:
		return InputTypeVideoOnly, true
	default:
		return UndefinedInputType, false
	}
}

func (s *StreamMux[C]) fanOutMode() (fanout.Mode, error) {
	return fanOutModeForMuxMode(s.MuxMode)
}

func (s *StreamMux[C]) inputForRouteID(
	routeID id.RouteID,
) (*Input[C], bool) {
	inputType, ok := inputTypeForRouteID(routeID)
	if !ok {
		return nil, false
	}

	switch inputType {
	case InputTypeAll:
		return &s.InputAll, true
	case InputTypeAudioOnly:
		return s.InputAudioOnly, s.InputAudioOnly != nil
	case InputTypeVideoOnly:
		return s.InputVideoOnly, s.InputVideoOnly != nil
	default:
		return nil, false
	}
}

func (s *StreamMux[C]) routeIDForInput(
	input *Input[C],
) (id.RouteID, error) {
	if input == nil {
		return "", fmt.Errorf("input is nil")
	}
	return routeIDForInputType(input.GetType())
}

func (s *StreamMux[C]) routeIDForOutput(
	output *Output[C],
) (id.RouteID, bool) {
	if output == nil {
		return "", false
	}
	switch output.InputFrom {
	case s.InputAll.Node:
		return streamMuxRouteAll, true
	case nil:
		return "", false
	default:
	}
	if s.InputAudioOnly != nil && output.InputFrom == s.InputAudioOnly.Node {
		return streamMuxRouteAudioOnly, true
	}
	if s.InputVideoOnly != nil && output.InputFrom == s.InputVideoOnly.Node {
		return streamMuxRouteVideoOnly, true
	}
	return "", false
}

func (s *StreamMux[C]) preferredRoutePlans(
	ctx context.Context,
	senderKey SenderKey,
) ([]fanout.RoutePlan[SenderKey], error) {
	if s.MuxMode == types.MuxModeDifferentOutputsSameTracksSplitAV &&
		senderKey.VideoCodec == "" &&
		senderKey.AudioCodec == "" {
		return nil, nil
	}

	mode, err := s.fanOutMode()
	if err != nil {
		return nil, err
	}
	planner := fanout.NewRoutePlanner[SenderKey](
		mode,
		streamMuxRouteAll,
		fanout.PreferredRoutePlannerFunc[SenderKey](func(
			_ context.Context,
			requested SenderKey,
		) ([]fanout.RoutePlan[SenderKey], error) {
			return splitAVPreferredRoutePlans(requested), nil
		}),
	)
	return planner.PlanPreferred(ctx, senderKey)
}

func splitAVPreferredRoutePlans(
	senderKey SenderKey,
) []fanout.RoutePlan[SenderKey] {
	var plans []fanout.RoutePlan[SenderKey]
	if senderKey.VideoCodec != "" {
		plans = append(plans, fanout.RoutePlan[SenderKey]{
			RouteID: streamMuxRouteVideoOnly,
			StorageKey: SenderKey{
				VideoCodec:      senderKey.VideoCodec,
				VideoResolution: senderKey.VideoResolution,
			},
		})
	}
	if senderKey.AudioCodec != "" {
		plans = append(plans, fanout.RoutePlan[SenderKey]{
			RouteID: streamMuxRouteAudioOnly,
			StorageKey: SenderKey{
				AudioCodec:      senderKey.AudioCodec,
				AudioSampleRate: senderKey.AudioSampleRate,
			},
		})
	}
	return plans
}

func senderKeySafeFormatter() safekey.Formatter[SenderKey] {
	return safekey.FormatterFunc[SenderKey](func(_ context.Context, key SenderKey) string {
		return key.String()
	})
}

func (s *StreamMux[C]) preferredFanOutState(
	ctx context.Context,
) (
	*route.Registry,
	*member.Registry[SenderKey, *Output[C]],
	*attachment.Index,
	error,
) {
	routes := route.NewRegistry()
	if err := s.ForEachInput(ctx, func(ctx context.Context, input *Input[C]) error {
		routeID, err := s.routeIDForInput(input)
		if err != nil {
			return err
		}
		return routes.Add(ctx, route.State{
			ID:   routeID,
			Pair: input.outputPair,
		})
	}); err != nil {
		return nil, nil, nil, err
	}

	attachments := attachment.NewIndex(func(ctx context.Context, routeID id.RouteID) bool {
		_, ok := routes.Load(ctx, routeID)
		return ok
	})
	members := member.NewRegistry[SenderKey, *Output[C]]()
	var errs []error
	s.OutputsMap.Range(func(_ SenderKey, output *Output[C]) bool {
		if output == nil || output.IsClosed() {
			return true
		}
		routeID, ok := s.routeIDForOutput(output)
		if !ok {
			errs = append(errs, fmt.Errorf("unable to resolve route for output %d", output.ID))
			return true
		}
		entry, err := members.Put(ctx, id.MemberID(output.ID), output.StorageKey(), output)
		if err != nil {
			errs = append(errs, fmt.Errorf("put output %d in fanout member registry: %w", output.ID, err))
			return true
		}
		if err := attachments.Attach(ctx, routeID, entry.ID); err != nil {
			errs = append(errs, fmt.Errorf("attach output %d to route %q: %w", output.ID, routeID, err))
		}
		return true
	})
	if err := errors.Join(errs...); err != nil {
		return nil, nil, nil, err
	}

	return routes, members, attachments, nil
}

func (s *StreamMux[C]) switchPreferredPlans(
	ctx context.Context,
	plans []fanout.RoutePlan[SenderKey],
) error {
	if len(plans) == 0 {
		return nil
	}
	for _, plan := range plans {
		output, _ := s.OutputsMap.Load(plan.StorageKey)
		if output == nil {
			return fmt.Errorf("output with key %s not found", plan.StorageKey)
		}
	}

	routes, members, attachments, err := s.preferredFanOutState(ctx)
	if err != nil {
		return err
	}
	switcher := fanout.NewPreferenceSwitcher[SenderKey, *Output[C]](senderKeySafeFormatter())
	err = switcher.SwitchPreferred(ctx, plans, routes, members, attachments)
	return s.translatePreferredSwitchError(ctx, plans, err)
}

func (s *StreamMux[C]) translatePreferredSwitchError(
	ctx context.Context,
	plans []fanout.RoutePlan[SenderKey],
	err error,
) error {
	if err == nil {
		return nil
	}

	var inProgress fanout.ErrSwitchAlreadyInProgress
	if errors.As(err, &inProgress) {
		return ErrSwitchAlreadyInProgress{
			OutputIDCurrent: OutputID(inProgress.SwitchMemberID),
			OutputIDNext:    OutputID(inProgress.SyncerMemberID),
		}
	}

	var alreadyPreferred fanout.ErrAllAlreadyPreferred
	if errors.As(err, &alreadyPreferred) {
		outputIDs := s.outputIDsForPlans(ctx, plans)
		if len(outputIDs) == 1 {
			return ErrOutputAlreadyPreferred{OutputID: outputIDs[0]}
		}
		return ErrOutputsAlreadyPreferred{OutputIDs: outputIDs}
	}

	return err
}

func (s *StreamMux[C]) outputIDsForPlans(
	_ context.Context,
	plans []fanout.RoutePlan[SenderKey],
) []OutputID {
	outputIDs := make([]OutputID, 0, len(plans))
	for _, plan := range plans {
		output, _ := s.OutputsMap.Load(plan.StorageKey)
		if output == nil {
			outputIDs = append(outputIDs, 0)
			continue
		}
		outputIDs = append(outputIDs, output.ID)
	}
	return outputIDs
}

func (s *StreamMux[C]) setPreferredOutputs(
	ctx context.Context,
	senderKey SenderKey,
) (_err error) {
	logger.Debugf(ctx, "setPreferredOutputs(ctx, %s)", senderKey)
	defer func() { logger.Debugf(ctx, "/setPreferredOutputs(ctx, %s): %v", senderKey, _err) }()

	if !s.IsAllowedDifferentOutputs() {
		return fmt.Errorf("unable to set preferred output in mux mode %s", s.MuxMode)
	}

	plans, err := s.preferredRoutePlans(ctx, senderKey)
	if err != nil {
		return fmt.Errorf("unable to plan preferred outputs for key %s: %w", senderKey, err)
	}
	if err := s.switchPreferredPlans(ctx, plans); err != nil {
		return fmt.Errorf("unable to set preferred outputs for key %s: %w", senderKey, err)
	}
	return nil
}

func (s *StreamMux[C]) setPreferredOutputForInput(
	ctx context.Context,
	input *Input[C],
	outputKey SenderKey,
) (_err error) {
	inputType := input.GetType()
	logger.Debugf(ctx, "setPreferredOutputForInput(ctx, %s, %s)", inputType, outputKey)
	defer func() { logger.Debugf(ctx, "/setPreferredOutputForInput(ctx, %s, %s): %v", inputType, outputKey, _err) }()

	if !s.IsAllowedDifferentOutputs() {
		return fmt.Errorf("setting preferred output is not allowed in mux mode %s", s.MuxMode)
	}

	routeID, err := s.routeIDForInput(input)
	if err != nil {
		return err
	}
	plans := []fanout.RoutePlan[SenderKey]{{
		RouteID:    routeID,
		StorageKey: outputKey,
	}}
	if err := s.switchPreferredPlans(ctx, plans); err != nil {
		return fmt.Errorf("unable to switch to the preferred output %s: %w", outputKey, err)
	}

	return nil
}

// recreateEvictedOutputDefault is the production implementation behind
// recreateEvictedOutputFunc. It materialises a fresh Output under the
// dead output's SenderKey and switches the orphaned input to it so a
// recoverable sender/encoder failure does not leave the input wedged at
// MinInt32 + StateDrop.
//
// Caller (recommitDemotedInputToSibling no-sibling branch) MUST hold no
// lock — this method takes s.Locker for the createAndConfigureOutputs +
// setPreferredOutputForInput pair, mirroring the locking discipline in
// switchToOutputByProps. Holding s.Locker across both calls keeps the
// pair atomic against a concurrent SwitchToOutputByProps that could
// otherwise observe the freshly-created Output mid-recreate and race
// it onto the input.
//
// The new Output's encoder inherits the StreamMux-level RawFrameSource
// flag, so raw-frame inputs keep the same encoder pixel-format policy
// after recovery as they had before the eviction.
func (s *StreamMux[C]) recreateEvictedOutputDefault(
	ctx context.Context,
	input *Input[C],
	deadOutputKey SenderKey,
) (_err error) {
	logger.Tracef(ctx, "recreateEvictedOutputDefault(%s, %s)", input.GetType(), deadOutputKey)
	defer func() {
		logger.Tracef(ctx, "/recreateEvictedOutputDefault(%s, %s): %v", input.GetType(), deadOutputKey, _err)
	}()

	s.Locker.Do(ctx, func() {
		transcoderConfig := s.CurrentOutputProps.TranscoderConfig
		if err := s.createAndConfigureOutputs(ctx, deadOutputKey, transcoderConfig); err != nil {
			_err = fmt.Errorf("createAndConfigureOutputs(%s): %w", deadOutputKey, err)
			return
		}

		// Switch the orphaned input(s) to the freshly-created Output.
		// Without this, the OutputSwitch / OutputSyncer remain at
		// MinInt32 and the new Output never receives any traffic — the
		// recreate would be a no-op from the input's perspective.
		//
		// setPreferredOutputs (not setPreferredOutputForInput): in
		// SplitAV mode the dead output's StorageKey is a split key
		// (video-only OR audio-only), and setPreferredOutputs
		// already decomposes via getVideoInput()/getAudioInput() —
		// matching the storage-key decomposition done by
		// getInputsForSenderKey at the createAndConfigureOutputs
		// callsite above. Passing the split key to
		// setPreferredOutputForInput with a fixed `input` picks the
		// wrong input in InputAll-vs-Video/AudioOnly modes; threading
		// the input choice through the canonical setPreferredOutputs
		// path is more robust against future storage-key semantics
		// changes.
		//
		// ErrOutputAlreadyPreferred / ErrOutputsAlreadyPreferred can
		// happen if a concurrent path switched the input first
		// (rare); treat as success.
		err := s.setPreferredOutputs(ctx, deadOutputKey)
		switch {
		case err == nil:
		case errors.As(err, &ErrOutputAlreadyPreferred{}):
			logger.Debugf(ctx, "input %s already preferred to %s", input.GetType(), deadOutputKey)
		case errors.As(err, &ErrOutputsAlreadyPreferred{}):
			logger.Debugf(ctx, "inputs already preferred to %s (orphaned was %s)", deadOutputKey, input.GetType())
		default:
			_err = fmt.Errorf("setPreferredOutputs(%s) for orphaned input %s: %w", deadOutputKey, input.GetType(), err)
		}
	})
	return _err
}

// lastEvictedKeyFor returns the SenderKey of the most-recently-evicted
// output for an orphaned input. Used by the 1 Hz retry tick to recover
// the routing state when no on-eviction event is left to drive it.
func (s *StreamMux[C]) lastEvictedKeyFor(input *Input[C]) (SenderKey, bool) {
	return s.lastEvictedKey.Load(input)
}

func (s *StreamMux[C]) GetOrCreateOutput(
	ctx context.Context,
	outputKey types.SenderKey,
	opts ...InitOutputOption,
) (output *Output[C], _isNew bool, _err error) {
	logger.Tracef(ctx, "GetOrCreateOutput(%#+v)", outputKey)
	defer func() { logger.Tracef(ctx, "/GetOrCreateOutput(%#+v): %v", outputKey, _err) }()
	return xsync.DoR3(ctx, &s.OutputsLocker, func() (*Output[C], bool, error) {
		return s.getOrCreateOutputLocked(ctx, outputKey, opts)
	})
}

func (s *StreamMux[C]) countOutputs() int {
	count := 0
	s.Outputs.Range(func(_ OutputID, output *Output[C]) bool {
		count++
		return true
	})
	return count
}

func (s *StreamMux[C]) existingFanOutMembers(
	_ context.Context,
) []fanout.ExistingMember[SenderKey] {
	var existing []fanout.ExistingMember[SenderKey]
	s.Outputs.Range(func(_ OutputID, output *Output[C]) bool {
		if output == nil || output.IsClosed() {
			return true
		}
		existing = append(existing, fanout.ExistingMember[SenderKey]{
			ID:         id.MemberID(output.ID),
			StorageKey: output.StorageKey(),
		})
		return true
	})
	slices.SortFunc(existing, func(
		left fanout.ExistingMember[SenderKey],
		right fanout.ExistingMember[SenderKey],
	) int {
		switch {
		case left.ID < right.ID:
			return -1
		case left.ID > right.ID:
			return 1
		default:
			return 0
		}
	})
	return existing
}

func (s *StreamMux[C]) planOutputCreation(
	ctx context.Context,
	outputKey SenderKey,
) (fanout.CreationDecision[SenderKey], error) {
	switch s.MuxMode {
	case types.MuxModeSameOutputSameTracks, types.MuxModeSameOutputDifferentTracks:
		if count := s.countOutputs(); count > 1 {
			return fanout.CreationDecision[SenderKey]{}, fmt.Errorf("mux mode %s allows only one output, but already have %d outputs", s.MuxMode, count)
		}
	default:
	}
	mode, err := s.fanOutMode()
	if err != nil {
		return fanout.CreationDecision[SenderKey]{}, err
	}
	planner := fanout.NewCreationPlanner[SenderKey](mode, streamMuxRouteAll)
	return planner.PlanCreate(ctx, outputKey, s.existingFanOutMembers(ctx))
}

func (s *StreamMux[C]) inputForNewOutputKey(
	_ context.Context,
	outputKey SenderKey,
) (*Input[C], error) {
	switch s.MuxMode {
	case types.MuxModeForbid,
		types.MuxModeSameOutputSameTracks,
		types.MuxModeSameOutputDifferentTracks,
		types.MuxModeDifferentOutputsSameTracks:
		return &s.InputAll, nil
	case types.MuxModeDifferentOutputsSameTracksSplitAV:
		switch {
		case outputKey.AudioCodec != "" && outputKey.VideoCodec != "":
			return nil, fmt.Errorf("in mux mode '%s', you can get video xor audio output, not both (acodec:%s, vcodec:%s)", s.MuxMode, outputKey.AudioCodec, outputKey.VideoCodec)
		case outputKey.AudioCodec != "":
			return s.InputAudioOnly, nil
		case outputKey.VideoCodec != "":
			return s.InputVideoOnly, nil
		default:
			return nil, fmt.Errorf("in mux mode '%s', you must specify either audio or video codec in output key", s.MuxMode)
		}
	default:
		return nil, fmt.Errorf("unknown mux mode: %s", s.MuxMode)
	}
}

// outputURLMatchesFactoryPreview implements Task #174 SetOutputURL
// drift detection on the Reuse path. Returns true when:
//   - the SenderFactory does NOT implement SenderURLPreviewer (the
//     factory cannot tell us what URL it would generate, so drift
//     detection is opt-in; absent the capability, the regular Reuse
//     semantics are preserved); OR
//   - the existing Output was constructed by a factory that did not
//     expose URL preview at the time (senderURLAtCreation is empty
//     and there is no captured URL to compare against); OR
//   - the factory's preview lookup errors out OR returns empty (treat
//     as "preview not currently available", preserve Reuse); OR
//   - the previewed URL matches the URL the existing Output recorded
//     at its construction.
//
// Returns false ONLY when both sides have a non-empty URL and they
// differ — that is the unambiguous "URL drift" signal that triggers
// teardown-and-recreate in getOrCreateOutputLocked.
func (s *StreamMux[C]) outputURLMatchesFactoryPreview(
	ctx context.Context,
	output *Output[C],
	outputKey types.SenderKey,
) bool {
	if output == nil || output.senderURLAtCreation == "" {
		return true
	}
	previewer, ok := s.SenderFactory.(SenderURLPreviewer)
	if !ok {
		return true
	}
	wantURL, err := previewer.URLForKey(ctx, outputKey)
	if err != nil || wantURL == "" {
		return true
	}
	return wantURL == output.senderURLAtCreation
}

func (s *StreamMux[C]) getOrCreateOutputLocked(
	ctx context.Context,
	outputKey types.SenderKey,
	opts []InitOutputOption,
) (_ret *Output[C], _isNew bool, _err error) {
	logger.Debugf(ctx, "getOrCreateOutputLocked: %#+v, %#+v", outputKey, opts)
	defer func() {
		logger.Debugf(ctx, "/getOrCreateOutputLocked: %#+v, %#+v: %v, %v", outputKey, opts, _ret, _err)
	}()

	if outputKey.VideoResolution == (codec.Resolution{}) && outputKey.VideoCodec != "" && outputKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, false, fmt.Errorf("output resolution is not set (codec: %s)", outputKey.VideoCodec)
	}
	if outputKey.AudioSampleRate == 0 && outputKey.AudioCodec != "" && outputKey.AudioCodec != codectypes.Name(codec.NameCopy) {
		return nil, false, fmt.Errorf("output audio sample rate is not set (codec: %s)", outputKey.AudioCodec)
	}

	outputID := OutputID(s.nextOutputID.Add(1))

	decision, err := s.planOutputCreation(ctx, outputKey)
	if err != nil {
		return nil, false, err
	}
	switch decision.Action {
	case fanout.CreationActionReuse:
		output, ok := s.Outputs.Load(OutputID(decision.ReuseMemberID))
		if !ok || output == nil || output.IsClosed() {
			return nil, false, fmt.Errorf("planned reuse of missing output %d for key %s", decision.ReuseMemberID, outputKey)
		}
		// Task #174 SetOutputURL drift detection. When the SenderFactory
		// exposes URL preview AND the URL it would now generate for this
		// senderKey differs from the URL captured at this Output's
		// construction, the factory's underlying URL template changed
		// since the existing sender was built (typically because of
		// SetOutputURL between the prior switch and this one). The
		// existing sender is publishing to the stale URL; reusing it
		// silently drops the operator's URL change. Tear down the
		// stale Output and fall through to the Create path below so
		// NewSender picks up the new URL.
		if !s.outputURLMatchesFactoryPreview(ctx, output, outputKey) {
			logger.Debugf(ctx, "Output %d:%s URL drift detected; tearing down before recreate", output.ID, outputKey)
			if removeErr := s.removeOutputLocked(ctx, output.StorageKey()); removeErr != nil {
				logger.Errorf(ctx, "unable to remove drift-stale output %d:%s: %v", output.ID, outputKey, removeErr)
			}
			// CloseNoDrain matches the replacing/discarding-outputs
			// precedent at L459 (Switch[inputType] outputPrev) and
			// L579 (ResetOutputsLocked): the drift-stale output
			// is being replaced, NOT gracefully shut down. The L644
			// graceful-shutdown Close is the exception, not the
			// precedent. Drain on a wedged-stale-URL output is futile
			// (the publish destination doesn't match any AVD consumer
			// — frames have nowhere to drain to) and would hold
			// StreamMux.locker unnecessarily during the bounded-but-
			// non-zero drain wait.
			if closeErr := output.CloseNoDrain(ctx); closeErr != nil {
				logger.Errorf(ctx, "unable to close drift-stale output %d:%s: %v", output.ID, outputKey, closeErr)
			}
			break
		}
		return output, false, nil
	case fanout.CreationActionReject:
		if s.MuxMode == types.MuxModeForbid {
			return nil, false, fmt.Errorf("mux mode %s forbids adding new outputs, but already have %d outputs", s.MuxMode, s.countOutputs())
		}
		return nil, false, fmt.Errorf("mux mode %s rejected adding output %s", s.MuxMode, outputKey)
	case fanout.CreationActionCreate:
	default:
		return nil, false, fmt.Errorf("unknown output creation action %d", decision.Action)
	}

	input, err := s.inputForNewOutputKey(ctx, outputKey)
	if err != nil {
		return nil, false, err
	}

	// Inherit StreamMux-level RawFrameSource as a default. Caller-supplied
	// opts apply on top, so an explicit OptionRawFrameSource(false) on a
	// specific GetOrCreateOutput call still wins (last-writer in
	// InitOutputOptions.apply).
	allOpts := make([]InitOutputOption, 0, len(opts)+1)
	if s.RawFrameSource.Load() {
		allOpts = append(allOpts, OptionRawFrameSource(true))
	}
	allOpts = append(allOpts, opts...)
	cfg := InitOutputOptions(allOpts).config()

	output, err := newOutput(
		ctx,
		outputID,
		input.Node,
		s.SenderFactory, outputKey,
		input.OutputSwitch.Output(int32(outputID)),
		input.OutputSyncer.Output(int32(outputID)),
		&s.allowCorruptPackets,
		input.MonotonicPTSFilter,
		newStreamIndexAssigner(s.MuxMode, outputID, input.Node.Processor.Kernel),
		s,
		s.asCodecResourceManager(),
		s,
		cfg,
	)
	if err != nil {
		return nil, true, fmt.Errorf("unable to create an output: %w", err)
	}
	s.Outputs.Store(outputID, output)
	s.OutputsMap.Store(outputKey, output)

	logger.Debugf(ctx, "initialized new output %d:%s", output.ID, outputKey)
	return output, true, nil
}

func (s *StreamMux[C]) EnableVideoTranscodingBypass(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "EnableVideoTranscodingBypass")
	defer func() { logger.Tracef(ctx, "/EnableVideoTranscodingBypass: %v", _err) }()
	return xsync.DoA1R1(ctx, &s.Locker, s.enableVideoTranscodingBypassLocked, ctx)
}

func (s *StreamMux[C]) enableVideoTranscodingBypassLocked(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "enableVideoTranscodingBypassLocked")
	defer func() { logger.Tracef(ctx, "/enableVideoTranscodingBypassLocked: %v", _err) }()

	if !s.IsAllowedDifferentOutputs() {
		return fmt.Errorf("video transcoding bypass is only allowed in mux modes with different outputs, but current mux mode is %s", s.MuxMode)
	}

	prevVideoOutput := s.GetActiveVideoOutput(ctx)

	var senderKey SenderKey
	var transcoderCfg types.TranscoderConfig
	switch s.MuxMode {
	case types.MuxModeDifferentOutputsSameTracks:
		senderKey = SenderKey{
			AudioCodec: codectypes.Name(codec.NameCopy),
			VideoCodec: codectypes.Name(codec.NameCopy),
		}
		transcoderCfg = types.TranscoderConfig{
			Output: types.TranscoderOutputConfig{
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
		transcoderCfg = types.TranscoderConfig{
			Output: types.TranscoderOutputConfig{
				VideoTrackConfigs: []types.OutputVideoTrackConfig{{
					CodecName: codectypes.Name(codec.NameCopy),
				}},
			},
		}
	default:
		return fmt.Errorf("unable to enable video transcoding bypass in mux mode %s", s.MuxMode)
	}

	err := s.createAndConfigureOutputs(ctx, senderKey, transcoderCfg)
	if err != nil {
		return fmt.Errorf("unable to initialize and prepare outputs for bypass key %s: %w", senderKey, err)
	}

	err = s.setPreferredOutputs(ctx, senderKey)
	switch {
	case err == nil:
	case errors.As(err, &ErrOutputAlreadyPreferred{}):
		return nil
	case errors.As(err, &ErrOutputsAlreadyPreferred{}):
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

func (s *StreamMux[C]) GetTranscoderConfig(
	ctx context.Context,
) (_ret types.TranscoderConfig) {
	logger.Tracef(ctx, "GetTranscoderConfig")
	defer func() { logger.Tracef(ctx, "/GetTranscoderConfig: %v", _ret) }()
	if s == nil {
		return types.TranscoderConfig{}
	}
	outputConfig := xsync.DoA1R1(ctx, &s.Locker, s.getCurrentOutputPropsLocked, ctx)
	return outputConfig.TranscoderConfig
}

func (s *StreamMux[C]) getCurrentOutputPropsLocked(
	_ context.Context,
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
	logger.Tracef(ctx, "switchToOutputByProps: %#+v, %v", props, persistent)
	defer func() {
		logger.Tracef(ctx, "/switchToOutputByProps: %#+v, %v: %v", props, persistent, _err)
	}()
	senderKey := PartialSenderKeyFromTranscoderConfig(ctx, &props.TranscoderConfig)
	previousOutputProps := s.CurrentOutputProps
	// Output recovery can run while this switch is still constructing
	// replacement outputs, so make the requested props visible for the
	// whole transition and roll them back only when the transition does
	// not become the mux's persistent configuration.
	s.CurrentOutputProps = props
	defer func() {
		if _err != nil || !persistent {
			s.CurrentOutputProps = previousOutputProps
		}
	}()

	if !s.IsAllowedDifferentOutputs() {
		if s.countOutputs() != 0 {
			return fmt.Errorf("mux mode %s forbids having more than one output", s.MuxMode)
		}
		if err := s.createAndConfigureOutput(ctx, &s.InputAll, senderKey, props.TranscoderConfig); err != nil {
			return fmt.Errorf("unable to create default output: %w", err)
		}
		s.InputAll.OutputSwitch.CurrentValue.Store(1)
		s.InputAll.OutputSyncer.CurrentValue.Store(1)
		return nil
	}

	err := s.createAndConfigureOutputs(ctx, senderKey, props.TranscoderConfig)
	if err != nil {
		return fmt.Errorf("unable to initialize and prepare outputs for key %s: %w", senderKey, err)
	}

	err = s.setPreferredOutputs(ctx, senderKey)
	switch {
	case err == nil:
	case errors.As(err, &ErrOutputAlreadyPreferred{}):
		return nil
	case errors.As(err, &ErrOutputsAlreadyPreferred{}):
		return nil
	default:
		return fmt.Errorf("unable to set the preferred outputs %s: %w", senderKey, err)
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
	if senderKey.VideoResolution == (codec.Resolution{}) && senderKey.VideoCodec != "" && senderKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output resolution is not set (codec: %s)", senderKey.VideoCodec)
	}
	if senderKey.AudioSampleRate == 0 && senderKey.AudioCodec != "" && senderKey.AudioCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output audio sample rate is not set (codec: %s)", senderKey.AudioCodec)
	}

	plans, err := s.preferredRoutePlans(ctx, senderKey)
	if err != nil {
		return nil, fmt.Errorf("unable to plan outputs in mux mode %s: %w", s.MuxMode, err)
	}

	inputsAndKeys := make([]inputAndKey[C], 0, len(plans))
	for _, plan := range plans {
		input, ok := s.inputForRouteID(plan.RouteID)
		if !ok {
			return nil, fmt.Errorf("unable to resolve input for route %q", plan.RouteID)
		}
		inputsAndKeys = append(inputsAndKeys, inputAndKey[C]{
			Input: input,
			Key:   plan.StorageKey,
		})
	}
	return inputsAndKeys, nil
}

func (s *StreamMux[C]) createAndConfigureOutputs(
	ctx context.Context,
	senderKey SenderKey,
	transcoderConfig types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "createAndConfigureOutputs(ctx, %s, %s)", senderKey, transcoderConfig)
	defer func() {
		logger.Tracef(ctx, "/createAndConfigureOutputs(ctx, %s, %s): %v", senderKey, transcoderConfig, _err)
	}()

	inputsAndKeys, err := s.getInputsForSenderKey(ctx, senderKey)
	if err != nil {
		return fmt.Errorf("unable to get inputs for sender key %s: %w", senderKey, err)
	}

	for idx, ik := range inputsAndKeys {
		if err := s.createAndConfigureOutput(ctx, ik.Input, ik.Key, transcoderConfig); err != nil {
			return fmt.Errorf("unable to create and configure output for input %d (%s) with key %s: %w", idx, ik.Input.GetType(), ik.Key, err)
		}
	}
	return nil
}

func (s *StreamMux[C]) createAndConfigureOutput(
	ctx context.Context,
	input *Input[C],
	senderKey SenderKey,
	transcoderConfig types.TranscoderConfig,
) (_err error) {
	logger.Tracef(ctx, "createAndConfigureOutput(ctx, %s)", senderKey)
	defer func() { logger.Tracef(ctx, "/createAndConfigureOutput(ctx, %s): %v", senderKey, _err) }()

	var output *Output[C]
	var isNew bool
	var err error
	s.OutputsLocker.Do(ctx, func() {
		output, isNew, err = s.getOrCreateOutputLocked(ctx, senderKey, nil)
	})
	if err != nil {
		return fmt.Errorf("unable to get-or-create the output %s: %w", senderKey, err)
	}

	logger.Debugf(ctx, "reconfiguring the output %d:%s (isNew: %v)", output.ID, senderKey, isNew)
	inputTranscoderConfig := transcoderConfigForInputType(input.GetType(), transcoderConfig)
	if !transcoderConfigHasOutputTracks(inputTranscoderConfig) {
		if isNew {
			s.removeUnconfiguredOutput(ctx, output)
		}
		return fmt.Errorf("no output tracks remain for input %s after split route selection", input.GetType())
	}
	err = output.reconfigureTranscoder(ctx, inputTranscoderConfig)
	if err != nil {
		if isNew {
			s.removeUnconfiguredOutput(ctx, output)
		}
		return fmt.Errorf("unable to reconfigure the output %d:%s: %w", output.ID, senderKey, err)
	}
	if isNew {
		input.Node.AddPushTo(ctx, output.Input())
	}

	output.InputFilter.Locker.ManualLock(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		defer output.InputFilter.Locker.ManualUnlock(ctx)
		logger.Debugf(ctx, "notifying about the sources")
		err := avpipeline.NotifyAboutPacketSources(ctx,
			input.Node.Processor.Kernel,
			output.FirstNodeAfterFilter(),
		)
		if err != nil {
			logger.Errorf(ctx, "received an error while notifying nodes about packet sources (%s -> %s): %v", input.Node.Processor.Kernel, output.InputFilter, err)
		}
	})
	return nil
}

func transcoderConfigForInputType(
	inputType InputType,
	transcoderConfig types.TranscoderConfig,
) types.TranscoderConfig {
	cfg := transcoderConfig
	if transcoderConfig.Input != nil {
		inputCfg := *transcoderConfig.Input
		cfg.Input = &inputCfg
	}

	switch inputType {
	case InputTypeAudioOnly:
		cfg.Output.VideoTrackConfigs = nil
		if cfg.Input != nil {
			cfg.Input.VideoTrackConfigs = nil
		}
	case InputTypeVideoOnly:
		cfg.Output.AudioTrackConfigs = nil
		if cfg.Input != nil {
			cfg.Input.AudioTrackConfigs = nil
		}
	}

	return cfg
}

func transcoderConfigHasOutputTracks(
	transcoderConfig types.TranscoderConfig,
) bool {
	if len(transcoderConfig.Output.VideoTrackConfigs) > 0 {
		return true
	}
	return len(transcoderConfig.Output.AudioTrackConfigs) > 0
}

func (s *StreamMux[C]) removeUnconfiguredOutput(
	ctx context.Context,
	output *Output[C],
) {
	if output == nil {
		return
	}

	var removeErr error
	s.OutputsLocker.Do(ctx, func() {
		stored, ok := s.OutputsMap.Load(output.StorageKey())
		if !ok || stored != output {
			return
		}
		removeErr = s.removeOutputLocked(ctx, output.StorageKey())
	})
	if removeErr != nil {
		logger.Errorf(ctx, "unable to remove unconfigured output %d:%s: %v", output.ID, output.StorageKey(), removeErr)
	}
	if err := output.Close(ctx); err != nil {
		logger.Errorf(ctx, "unable to close unconfigured output %d:%s: %v", output.ID, output.StorageKey(), err)
	}
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
		tryGetStats(fmt.Sprintf("Output(%s):TranscoderNode", outputKey), output.TranscoderNode)
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
	defer func() { logger.Debugf(ctx, "/Start(ctx): %v", _err) }()

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
			case <-ctx.Done():
				logger.Debugf(ctx, "stopping listening for errors: %v", ctx.Err())
				return
			case err, ok := <-errCh:
				if !ok {
					logger.Debugf(ctx, "the error channel is closed")
					return
				}
				// Classify the error before cancelling: recoverable errors
				// should not tear down the pipeline.
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
				cancelFn()
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
	defer t.Stop()
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

func (s *StreamMux[C]) withActiveVideoOutput(
	ctx context.Context,
	callback func(output *Output[C]) error,
) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			var err error
			var done bool
			s.Locker.Do(ctx, func() {
				output := s.getActiveVideoOutputLocked(ctx)
				if output == nil {
					return
				}
				err = callback(output)
				done = true
			})
			if done {
				return err
			}
		}
	}
}

// GetActiveVideoOutput returns the currently active video output.
//
// Note: the "active" means gated in by OutputSyncer.
func (s *StreamMux[C]) GetActiveVideoOutput(
	ctx context.Context,
) *Output[C] {
	result := xsync.DoA1R1(ctx, &s.OutputsLocker, s.getActiveVideoOutputLocked, ctx)
	logger.Tracef(ctx, "GetActiveVideoOutput returning: %v", result != nil)
	return result
}

func (s *StreamMux[C]) getActiveVideoOutputLocked(
	ctx context.Context,
) *Output[C] {
	outputID := s.getVideoInput().OutputSwitch.CurrentValue.Load()
	logger.Tracef(ctx, "getActiveVideoOutputLocked: outputID=%d", outputID)
	output, _ := s.Outputs.Load(OutputID(outputID))
	logger.Tracef(ctx, "getActiveVideoOutputLocked: found output: %v", output != nil)
	return output
}

// GetActiveAudioOutput returns the currently active audio output.
//
// Note: the "active" means gated in by OutputSyncer.
func (s *StreamMux[C]) GetActiveAudioOutput(
	ctx context.Context,
) *Output[C] {
	return xsync.DoA1R1(ctx, &s.OutputsLocker, s.getActiveAudioOutputLocked, ctx)
}

func (s *StreamMux[C]) getActiveAudioOutputLocked(
	ctx context.Context,
) *Output[C] {
	outputID := s.getAudioInput().OutputSwitch.CurrentValue.Load()
	logger.Tracef(ctx, "getActiveAudioOutputLocked: outputID=%d", outputID)
	output, _ := s.Outputs.Load(OutputID(outputID))
	return output
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
		return fmt.Errorf("currently we support only exactly one output audio track config (have %d)", len(cfg.Output.AudioTrackConfigs))
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

	configuredVideoCodec := codectypes.Name(configuredCodecName(videoCfg.CodecNames, videoCfg.CodecName))
	configuredAudioCodec := codectypes.Name(configuredCodecName(audioCfg.CodecNames, audioCfg.CodecName))

	if videoCfg.Resolution == res && configuredVideoCodec == videoCodec && configuredAudioCodec == audioCodec {
		logger.Tracef(ctx, "the config is already set to %v '%s' '%s'", res, videoCodec, audioCodec)
		encoderV, _ := s.getVideoEncoderLocked(ctx)
		if videoCodec == codectypes.Name(codec.NameCopy) != codec.IsEncoderCopy(encoderV) {
			logger.Errorf(ctx, "the video codec is set to '%s', but the encoder is %s", videoCodec, encoderV)
		} else {
			// Apply the bitrate update via switchToOutputByProps so the change
			// propagates to the running encoder (previously the updated local copy
			// was discarded, making this a dead store).
			videoCfg.AverageBitRate = uint64(bitRate)
			cfg.Output.VideoTrackConfigs[0] = videoCfg
			err := s.switchToOutputByProps(ctx, types.SenderProps{
				TranscoderConfig: cfg.TranscoderConfig,
			}, true)
			if err != nil {
				return fmt.Errorf("unable to apply bitrate update: %w", err)
			}
			return ErrAlreadySet{}
		}
	}

	if configuredAudioCodec != audioCodec {
		audioCfg.CodecName = audioCodec
		audioCfg.CodecNames = nil
	}
	cfg.Output.AudioTrackConfigs[0] = audioCfg

	videoCfg.Resolution = res
	videoCfg.AverageBitRate = uint64(bitRate)
	if configuredVideoCodec != videoCodec {
		videoCfg.CodecName = videoCodec
		videoCfg.CodecNames = nil
	}
	cfg.Output.VideoTrackConfigs[0] = videoCfg

	err := s.switchToOutputByProps(ctx, types.SenderProps{
		TranscoderConfig: cfg.TranscoderConfig,
	}, true)
	if err != nil {
		return fmt.Errorf("unable to switch to the new output props: %w", err)
	}

	return nil
}

func (s *StreamMux[C]) SetFPSFraction(ctx context.Context, f globaltypes.Rational) {
	logger.Tracef(ctx, "SetFPSFraction: %s", f)
	if f.Num > math.MaxUint32 || f.Den > math.MaxUint32 {
		origFraction := f
		for f.Num > math.MaxUint32 || f.Den > math.MaxUint32 {
			f.Num /= 2
			f.Den /= 2
		}
		logger.Errorf(ctx, "FPS fraction %s is too large, scaled down to %s", origFraction, f)
	}
	newValue := (uint64(f.Num) << 32) | uint64(f.Den)
	oldValue := s.FPSFractionNumDen.Swap(newValue)
	if oldValue != newValue {
		logger.Infof(ctx, "SetFPSFraction: FPS fraction changed to %s", f)
	}
}

func (s *StreamMux[C]) GetFPSFraction(ctx context.Context) globaltypes.Rational {
	var r globaltypes.Rational
	numDen := s.FPSFractionNumDen.Load()
	r.Num = int(numDen >> 32)
	r.Den = int(numDen & 0xFFFFFFFF)
	if r.Den == 0 {
		// unset, yet
		return globaltypes.Rational{Num: 1, Den: 1}
	}
	return r
}

func (s *StreamMux[C]) inputBitRateMeasurerLoop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "inputBitRateMeasurerLoop")
	defer func() { logger.Tracef(ctx, "/inputBitRateMeasurerLoop: %v", _err) }()

	t := time.NewTicker(time.Second / 4)
	defer t.Stop()
	bytesInputReadPrev := map[astiav.MediaType]uint64{}
	bytesEncodedGenPrev := map[astiav.MediaType]uint64{}
	bytesOutputReadPrev := map[astiav.MediaType]uint64{}
	tsPrev := time.Now()
	for {
		var tsNext time.Time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tsNext = <-t.C:
			duration := tsNext.Sub(tsPrev)
			if duration <= 0 {
				tsPrev = tsNext
				continue
			}

			inputCounters := s.InputAll.Node.GetCountersPtr()
			bytesInputReadNext := map[astiav.MediaType]uint64{
				astiav.MediaTypeVideo:   inputCounters.Sent.Packets.Video.Bytes.Load() + inputCounters.Sent.Frames.Video.Bytes.Load(),
				astiav.MediaTypeAudio:   inputCounters.Sent.Packets.Audio.Bytes.Load() + inputCounters.Sent.Frames.Audio.Bytes.Load(),
				astiav.MediaTypeUnknown: inputCounters.Sent.Packets.Other.Bytes.Load() + inputCounters.Sent.Frames.Other.Bytes.Load(),
			}

			bytesEncodedGenNext := map[astiav.MediaType]uint64{}
			bytesOutputReadNext := map[astiav.MediaType]uint64{}
			s.OutputsMap.Range(func(outputKey SenderKey, output *Output[C]) bool {
				encoderCounters := output.TranscoderNode.Processor.CountersPtr()
				bytesEncodedGenNext[astiav.MediaTypeVideo] += encoderCounters.Generated.Packets.Video.Bytes.Load() + encoderCounters.Generated.Frames.Video.Bytes.Load() - (encoderCounters.Omitted.Packets.Video.Bytes.Load() + encoderCounters.Omitted.Frames.Video.Bytes.Load())
				bytesEncodedGenNext[astiav.MediaTypeAudio] += encoderCounters.Generated.Packets.Audio.Bytes.Load() + encoderCounters.Generated.Frames.Audio.Bytes.Load() - (encoderCounters.Omitted.Packets.Audio.Bytes.Load() + encoderCounters.Omitted.Frames.Audio.Bytes.Load())
				bytesEncodedGenNext[astiav.MediaTypeUnknown] += encoderCounters.Generated.Packets.Other.Bytes.Load() + encoderCounters.Generated.Frames.Other.Bytes.Load() - (encoderCounters.Omitted.Packets.Other.Bytes.Load() + encoderCounters.Omitted.Frames.Other.Bytes.Load())
				outputCounters := output.SendingNode.GetCountersPtr()
				bytesOutputReadNext[astiav.MediaTypeVideo] += outputCounters.Received.Packets.Video.Bytes.Load() + outputCounters.Received.Frames.Video.Bytes.Load()
				bytesOutputReadNext[astiav.MediaTypeAudio] += outputCounters.Received.Packets.Audio.Bytes.Load() + outputCounters.Received.Frames.Audio.Bytes.Load()
				bytesOutputReadNext[astiav.MediaTypeUnknown] += outputCounters.Received.Packets.Other.Bytes.Load() + outputCounters.Received.Frames.Other.Bytes.Load()
				return true
			})

			for _, mediaType := range []astiav.MediaType{astiav.MediaTypeVideo, astiav.MediaTypeAudio, astiav.MediaTypeUnknown} {
				m := s.getTrackMeasurements(mediaType)
				bytesInputRead := uint64(0)
				if bytesInputReadNext[mediaType] >= bytesInputReadPrev[mediaType] {
					bytesInputRead = bytesInputReadNext[mediaType] - bytesInputReadPrev[mediaType]
				}
				bitRateInput := int(float64(bytesInputRead*8) / duration.Seconds())
				oldInputValue := m.InputBitRate.Load()
				newInputValue := updateWithInertialValue(oldInputValue, uint64(bitRateInput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
				m.InputBitRate.Store(newInputValue)

				bytesEncodedGen := uint64(0)
				if bytesEncodedGenNext[mediaType] >= bytesEncodedGenPrev[mediaType] {
					bytesEncodedGen = bytesEncodedGenNext[mediaType] - bytesEncodedGenPrev[mediaType]
				}
				bitRateEncoded := int(float64(bytesEncodedGen*8) / duration.Seconds())
				oldEncodedValue := m.EncodedBitRate.Load()
				newEncodedValue := updateWithInertialValue(oldEncodedValue, uint64(bitRateEncoded), 0.9, s.CurrentBitRateMeasurementsCount.Load())
				m.EncodedBitRate.Store(newEncodedValue)

				bytesOutputRead := uint64(0)
				if bytesOutputReadNext[mediaType] >= bytesOutputReadPrev[mediaType] {
					bytesOutputRead = bytesOutputReadNext[mediaType] - bytesOutputReadPrev[mediaType]
				}
				bitRateOutput := int(float64(bytesOutputRead*8) / duration.Seconds())
				oldOutputValue := m.OutputBitRate.Load()
				newOutputValue := updateWithInertialValue(oldOutputValue, uint64(bitRateOutput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
				m.OutputBitRate.Store(newOutputValue)

				logger.Tracef(ctx, "inputBitRateMeasurerLoop: mediaType:%v, duration:%v, bytesInputRead:%v, bitRateInput:%v, oldInputBitRate:%v, newInputBitRate:%v, bytesEncodedGen:%v, bitRateEncoded:%v, oldEncodedBitRate:%v, newEncodedBitRate:%v, bytesOutputRead:%v, bitRateOutput:%v, oldOutputBitRate:%v, newOutputBitRate:%v (raw: inputNext:%v, inputPrev:%v, encodedNext:%v, encodedPrev:%v, outputNext:%v, outputPrev:%v)", mediaType, duration, bytesInputRead, bitRateInput, oldInputValue, newInputValue, bytesEncodedGen, bitRateEncoded, oldEncodedValue, newEncodedValue, bytesOutputRead, bitRateOutput, oldOutputValue, newOutputValue, bytesInputReadNext[mediaType], bytesInputReadPrev[mediaType], bytesEncodedGenNext[mediaType], bytesEncodedGenPrev[mediaType], bytesOutputReadNext[mediaType], bytesOutputReadPrev[mediaType])
			}

			// DONE

			s.CurrentBitRateMeasurementsCount.Add(1)

			bytesInputReadPrev = bytesInputReadNext
			bytesEncodedGenPrev = bytesEncodedGenNext
			bytesOutputReadPrev = bytesOutputReadNext
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
	_ context.Context,
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

	vEncoders := o.TranscoderNode.Processor.Kernel.EncoderFactory.VideoEncoders
	if len(vEncoders) == 1 {
		vEnc = vEncoders[0]
	}

	// In SplitAV mode the audio encoder lives on the audio output, not the
	// video output. Fetch it from there to avoid returning nil.
	audioSource := o
	if s.MuxMode == types.MuxModeDifferentOutputsSameTracksSplitAV {
		if ao := s.getActiveAudioOutputLocked(ctx); ao != nil {
			audioSource = ao
		}
	}

	aEncoders := audioSource.TranscoderNode.Processor.Kernel.EncoderFactory.AudioEncoders
	if len(aEncoders) == 1 {
		aEnc = aEncoders[0]
	}

	return vEnc, aEnc
}

func (s *StreamMux[C]) onInput(
	ctx context.Context,
	input packetorframe.InputUnion,
) (_err error) {
	abstract := input.Get()
	if abstract == nil {
		return nil
	}
	logger.Tracef(ctx, "onInput: %v", abstract.GetPTS())
	defer func() { logger.Tracef(ctx, "/onInput: %v: %v", abstract.GetPTS(), _err) }()

	if dtsInt := abstract.GetDTS(); dtsInt > 0 && dtsInt != astiav.NoPtsValue {
		dts := avconv.Duration(dtsInt, abstract.GetTimeBase())
		s.getTrackMeasurements(abstract.GetMediaType()).InputDTS.Store(uint64(dts.Nanoseconds()))
	}

	if abstract.GetMediaType() != astiav.MediaTypeVideo {
		return nil
	}

	isKey := abstract.IsKey()
	if !isKey {
		return nil
	}
	s.allowCorruptPackets.Store(false)

	streamIndex := abstract.GetStreamIndex()

	s.Locker.Do(ctx, func() {
		if s.lastKeyFrames[streamIndex] == nil {
			s.lastKeyFrames[streamIndex] = ringbuffer.New[packetorframe.InputUnion](2)
		}
		s.lastKeyFrames[streamIndex].Add(input.CloneAsReferencedInput())
	})
	return nil
}

func (s *StreamMux[C]) InitOutputVideoStreams(
	ctx context.Context,
	receiver node.Abstract,
	outputKey SenderKey,
) (_err error) {
	isBypass := outputKey.VideoCodec == codectypes.Name(codec.NameCopy)
	logger.Debugf(ctx, "InitOutputVideoStreams: isBypass:%v", isBypass)
	defer func() { logger.Debugf(ctx, "/InitOutputVideoStreams: isBypass:%v: %v", isBypass, _err) }()
	if !isBypass {
		return nil
	}
	return xsync.DoA2R1(ctx, &s.Locker, s.initOutputVideoStreamsLocked, ctx, receiver)
}

func (s *StreamMux[C]) initOutputVideoStreamsLocked(
	ctx context.Context,
	receiver node.Abstract,
) (_err error) {
	logger.Tracef(ctx, "initOutputVideoStreamsLocked")
	defer func() { logger.Tracef(ctx, "/initOutputVideoStreamsLocked: %v", _err) }()
	counters := receiver.GetCountersPtr()
	inputCh := receiver.GetProcessor().InputChan()
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
			dataSize := uint64(pkt.GetSize())
			if pkt.Packet != nil {
				counters.Addressed.Packets.Get(mediaType).Increment(dataSize)
			} else if pkt.Frame != nil {
				counters.Addressed.Frames.Get(mediaType).Increment(dataSize)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case inputCh <- pkt:
			}
			if pkt.Packet != nil {
				counters.Received.Packets.Get(mediaType).Increment(dataSize)
			} else if pkt.Frame != nil {
				counters.Received.Frames.Get(mediaType).Increment(dataSize)
			}
		}
	}
	return nil
}

func (s *StreamMux[C]) GetBitRates(
	ctx context.Context,
) (_ret *types.BitRates, _err error) {
	logger.Tracef(ctx, "GetBitRates")
	defer func() { logger.Tracef(ctx, "/GetBitRates: %v, %v", _ret, _err) }()

	video := s.getTrackMeasurements(astiav.MediaTypeVideo)
	audio := s.getTrackMeasurements(astiav.MediaTypeAudio)
	other := s.getTrackMeasurements(astiav.MediaTypeUnknown)

	result := &types.BitRates{
		Input: types.BitRateInfo{
			Video: types.Ubps(video.InputBitRate.Load()),
			Audio: types.Ubps(audio.InputBitRate.Load()),
			Other: types.Ubps(other.InputBitRate.Load()),
		},
		Encoded: types.BitRateInfo{
			Video: types.Ubps(video.EncodedBitRate.Load()),
			Audio: types.Ubps(audio.EncodedBitRate.Load()),
			Other: types.Ubps(other.EncodedBitRate.Load()),
		},
		Output: types.BitRateInfo{
			Video: types.Ubps(video.OutputBitRate.Load()),
			Audio: types.Ubps(audio.OutputBitRate.Load()),
			Other: types.Ubps(other.OutputBitRate.Load()),
		},
	}

	return result, nil
}

func nanosecondsToDuration(nanoseconds uint64) time.Duration {
	return time.Nanosecond * time.Duration(nanoseconds)
}

func (s *StreamMux[C]) updateSendingLatencyValues(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "updateSendingLatencyValues")
	defer func() { logger.Tracef(ctx, "/updateSendingLatencyValues: %v", _err) }()

	outputVideo := s.GetActiveVideoOutput(ctx)
	if outputVideo == nil {
		return fmt.Errorf("no active video output")
	}

	videoQueuer, ok := outputVideo.SendingNode.GetProcessor().(processortypes.UnsafeGetOldestDTSInTheQueuer)
	if !ok {
		return fmt.Errorf("unable to get the queuer from the output sending node processor %T", outputVideo.SendingNode.GetProcessor())
	}

	videoSendingOldestDTS, err := videoQueuer.GetOldestDTSInTheQueue(ctx)
	videoSendingEarliestDTS := time.Nanosecond * time.Duration(outputVideo.Measurements.LastSendingVideoDTS.Load())
	switch {
	case err == nil:
	case errors.As(err, &kernel.ErrApproximateValue{}):
		logger.Warnf(ctx, "receive an significantly imprecise video DTS value from the queuer")
	default:
		return fmt.Errorf("unable to get the oldest DTS in the video output queuer: %w", err)
	}
	logger.Tracef(ctx, "sending latencies: video output DTS: %v, video input DTS: %v", videoSendingOldestDTS, videoSendingEarliestDTS)
	var videoLatency time.Duration
	if videoSendingOldestDTS > 0 { // there is something in the queue
		videoLatency = videoSendingEarliestDTS - videoSendingOldestDTS
	}
	if videoLatency < 0 {
		logger.Tracef(ctx, "video latency is negative: %v; setting to 0", videoLatency)
		videoLatency = 0
	}
	s.getTrackMeasurements(astiav.MediaTypeVideo).SendingLatency.Store(uint64(videoLatency.Nanoseconds()))

	outputAudio := s.GetActiveAudioOutput(ctx)
	if outputAudio == nil {
		return fmt.Errorf("no active audio output")
	}

	audioQueuer, ok := outputAudio.SendingNode.GetProcessor().(processortypes.UnsafeGetOldestDTSInTheQueuer)
	if !ok {
		return fmt.Errorf("unable to get the queuer from the output sending node processor %T", outputAudio.SendingNode.GetProcessor())
	}
	audioSendingOldestDTS, err := audioQueuer.GetOldestDTSInTheQueue(ctx)
	audioSendingEarliestDTS := time.Nanosecond * time.Duration(outputAudio.Measurements.LastSendingAudioDTS.Load())
	switch {
	case err == nil:
	case errors.As(err, &kernel.ErrApproximateValue{}):
		logger.Warnf(ctx, "receive an significantly imprecise audio DTS value from the queuer")
	default:
		return fmt.Errorf("unable to get the oldest DTS in the audio output queuer: %w", err)
	}
	logger.Tracef(ctx, "sending latencies: audio output DTS: %v, audio input DTS: %v", audioSendingOldestDTS, audioSendingEarliestDTS)
	var audioLatency time.Duration
	if audioSendingOldestDTS > 0 { // there is something in the queue
		audioLatency = audioSendingEarliestDTS - audioSendingOldestDTS
	}
	if audioLatency < 0 {
		logger.Tracef(ctx, "audio latency is negative: %v; setting to 0", audioLatency)
		audioLatency = 0
	}
	s.getTrackMeasurements(astiav.MediaTypeAudio).SendingLatency.Store(uint64(audioLatency.Nanoseconds()))

	maxLatency := uint64(max(audioLatency, videoLatency).Nanoseconds())
	logger.Debugf(ctx, "sending latencies: video=%v audio=%v max=%v", videoLatency, audioLatency, time.Duration(maxLatency))
	return nil
}

func (s *StreamMux[C]) latencyMeasurerLoop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "latencyMeasurerLoop")
	defer func() { logger.Debugf(ctx, "/latencyMeasurerLoop: %v", _err) }()

	t := time.NewTicker(time.Second / 4)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			func() {
				ctx, cancelFn := context.WithTimeout(ctx, time.Second)
				defer cancelFn()
				// On error we previously fabricated a growing latency by adding
				// the wall-clock tick interval to SendingLatency, on the
				// assumption that "we cannot measure -> the queue must be
				// stalled and growing". That assumption silently turns any
				// transient measurement error (no active output, queuer not
				// ready, ctx timeout, ...) into an unbounded synthetic value
				// (observed: 226s, 535s) that misrepresents real latency to
				// the gRPC client and corrupts the autobitrate handler that
				// reads SendingLatency as queue duration. Leave the last
				// measured value in place instead — the next successful tick
				// will overwrite it with a fresh measurement.
				if err := s.updateSendingLatencyValues(ctx); err != nil {
					logger.Debugf(ctx, "unable to update sending latency values: %v; keeping the last measured value", err)
				}
			}()
		}
	}
}

func (s *StreamMux[C]) GetLatencies(
	ctx context.Context,
) (_ret *types.Latencies, _err error) {
	logger.Debugf(ctx, "GetLatencies")
	defer func() { logger.Debugf(ctx, "/GetLatencies: %v, %v", _ret, _err) }()

	outputVideo := s.GetActiveVideoOutput(ctx)
	if outputVideo == nil {
		return nil, fmt.Errorf("no active video output")
	}

	outputAudio := s.GetActiveAudioOutput(ctx)
	if outputAudio == nil {
		return nil, fmt.Errorf("no active audio output")
	}

	lastSendingAudioDTS, lastSendingVideoDTS := outputAudio.Measurements.LastSendingAudioDTS.Load(), outputVideo.Measurements.LastSendingVideoDTS.Load()

	transcodingEndAudioDTS, transcodingEndVideoDTS := outputAudio.Measurements.TranscodingEndAudioDTS.Load(), outputVideo.Measurements.TranscodingEndVideoDTS.Load()

	transcodingStartAudioDTS, transcodingStartVideoDTS := outputAudio.Measurements.TranscodingStartAudioDTS.Load(), outputVideo.Measurements.TranscodingStartVideoDTS.Load()

	video := s.getTrackMeasurements(astiav.MediaTypeVideo)
	audio := s.getTrackMeasurements(astiav.MediaTypeAudio)

	inputAudioDts, inputVideoDTS := audio.InputDTS.Load(), video.InputDTS.Load()

	// Guard against unsigned underflow: if the minuend is less than the
	// subtrahend the samples were collected out-of-order and the difference
	// is meaningless, so we leave the latency at zero.
	var audioPreTranscodingLatency time.Duration
	if inputAudioDts >= transcodingStartAudioDTS {
		audioPreTranscodingLatency = nanosecondsToDuration(inputAudioDts - transcodingStartAudioDTS)
	}
	var videoPreTranscodingLatency time.Duration
	if inputVideoDTS >= transcodingStartVideoDTS {
		videoPreTranscodingLatency = nanosecondsToDuration(inputVideoDTS - transcodingStartVideoDTS)
	}

	var audioTranscodingLatency time.Duration
	if transcodingStartAudioDTS >= transcodingEndAudioDTS {
		audioTranscodingLatency = nanosecondsToDuration(transcodingStartAudioDTS - transcodingEndAudioDTS)
	}
	var videoTranscodingLatency time.Duration
	if transcodingStartVideoDTS >= transcodingEndVideoDTS {
		videoTranscodingLatency = nanosecondsToDuration(transcodingStartVideoDTS - transcodingEndVideoDTS)
	}

	var audioTranscodedPreSendLatency time.Duration
	if transcodingEndAudioDTS >= lastSendingAudioDTS {
		audioTranscodedPreSendLatency = nanosecondsToDuration(transcodingEndAudioDTS - lastSendingAudioDTS)
	}
	var videoTranscodedPreSendLatency time.Duration
	if transcodingEndVideoDTS >= lastSendingVideoDTS {
		videoTranscodedPreSendLatency = nanosecondsToDuration(transcodingEndVideoDTS - lastSendingVideoDTS)
	}

	logger.Debugf(ctx, "latencies: audio: pre-encoding=%v transcoding=%v transcoded-pre-send=%v sending=%v", audioPreTranscodingLatency, audioTranscodingLatency, audioTranscodedPreSendLatency, nanosecondsToDuration(audio.SendingLatency.Load()))
	logger.Debugf(ctx, "latencies: video: pre-encoding=%v transcoding=%v transcoded-pre-send=%v (%v-%v) sending=%v", videoPreTranscodingLatency, videoTranscodingLatency, videoTranscodedPreSendLatency, transcodingEndVideoDTS, lastSendingVideoDTS, nanosecondsToDuration(video.SendingLatency.Load()))

	return &types.Latencies{
		Audio: types.TrackLatencies{
			PreTranscoding:    audioPreTranscodingLatency,
			Transcoding:       audioTranscodingLatency,
			TranscodedPreSend: audioTranscodedPreSendLatency,
			Sending:           nanosecondsToDuration(audio.SendingLatency.Load()),
		},
		Video: types.TrackLatencies{
			PreTranscoding:    videoPreTranscodingLatency,
			Transcoding:       videoTranscodingLatency,
			TranscodedPreSend: videoTranscodedPreSendLatency,
			Sending:           nanosecondsToDuration(video.SendingLatency.Load()),
		},
	}, nil
}

func (s *StreamMux[C]) IsAllowedDifferentOutputs() bool {
	mode, err := s.fanOutMode()
	if err != nil {
		return false
	}
	return fanout.NewDifferentOutputPolicy(mode).AllowsDifferentOutputs(context.Background())
}

type TrackMeasurements struct {
	InputBitRate   atomic.Uint64
	EncodedBitRate atomic.Uint64
	OutputBitRate  atomic.Uint64
	InputDTS       atomic.Uint64
	SendingLatency atomic.Uint64
}

func newTrackMeasurements() *TrackMeasurements {
	return &TrackMeasurements{}
}

func (s *StreamMux[C]) getTrackMeasurements(mediaType astiav.MediaType) *TrackMeasurements {
	m := s.Measurements[mediaType]
	if m != nil {
		return m
	}
	return s.Measurements[astiav.MediaTypeUnknown]
}
