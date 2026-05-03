// input_with_fallback.go implements a preset for an input with fallback sources.

// Package inputwithfallback provides a preset for an input with fallback sources.
package inputwithfallback

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/monotonicpts"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	debugConsistencyCheckLoop = true
)

type InputWithFallback[K InputKernel, DF codec.DecoderFactory, C any] struct {
	InputFilter         xatomic.Value[packetorframefiltercondition.Condition]
	InputChainsLocker   xsync.Mutex
	InputChains         []*InputChain[K, DF, C]
	InputSwitch         *barrierstategetter.Switch
	InputSyncer         *barrierstategetter.Switch
	MonotonicPTS        *monotonicpts.Filter
	PreOutput           *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Passthrough]]
	Output              *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Passthrough]]
	Config              Config
	AllowCorruptPackets atomic.Bool

	newInputChainChan chan *InputChain[K, DF, C]
	isServing         atomic.Bool
	serveWaitGroup    sync.WaitGroup
	syncingSince      xatomic.Value[time.Time]
	// switchingProcN counts in-flight switch-related work that gates
	// concurrent SetValue calls in OnSwitchRequest. It tracks both the
	// transient async goroutines (OnSwitchRequest unpause/pause workers,
	// OnAfterSwitch pausers — each defer-decrements) and the
	// OnBeforeSwitch → InputSyncer-KeepUnless cycle (one +1 per active
	// cycle, released when KeepUnless first matches OR when a fresh
	// switch supersedes a stuck cycle via syncingGen).
	switchingProcN xatomic.Int64
	// syncingGen tags the currently-pending OnBeforeSwitch →
	// InputSyncer-KeepUnless cycle. Zero means no cycle pending. Each
	// OnBeforeSwitch atomically Swaps in a fresh generation produced by
	// nextSyncingGen.Add(1); if the prior gen was non-zero the prior
	// cycle is superseded and the new cycle inherits its switchingProcN
	// reservation (no extra +1). The KeepUnless decrement and other
	// teardown sites (OnInterruptedSwitch, OnSwitchRequest's stuck-cycle
	// release) attempt Swap(0) and only act on a non-zero return — so a
	// stale cycle's defer becomes a no-op once the gen has been bumped.
	// This recovers from a stuck syncer (predicate never matches) by
	// letting a fresh switch request supersede it without a process
	// restart.
	syncingGen     xatomic.Uint64
	nextSyncingGen xatomic.Uint64

	// measurements
	Measurements                    map[astiav.MediaType]*TrackMeasurements
	CurrentBitRateMeasurementsCount atomic.Uint64
}

// New creates a new InputWithFallback instance.
//
// |  retryable:input0 -> inputSwitch (-> autoheaders -> decoder) -> inputSyncer ->-+
// |  (main)                   :                                          :         |
// |                           :                                          :         |               MonotonicPTS
// |  retryable:input1 -> inputSwitch (-> autoheaders -> decoder) -> inputSyncer ->-+-> Passthrough--------------> Passthrough -->--
// |  (fallback)               :                                          :         |                                          (one output)
// |                           :                                          :         |
// |  retryable:input2 -> inputSwitch (-> autoheaders -> decoder) -> inputSyncer ->-+
// |  (second fallback)        :                                          :         |
// |                           :                                          :         |
// |  ...                     ...          ...             ...           ...     ->-+
//
// K is the input kernel type (generally it is *kernel.Input).
// C is the custom data type associated with each input node (generally it is struct{}).
func New[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	inputFactories []InputFactory[K, DF, C],
	opts ...Option,
) (_ret *InputWithFallback[K, DF, C], _err error) {
	logger.Debugf(ctx, "New")
	defer func() { logger.Debugf(ctx, "/New: %v", _err) }()

	i := &InputWithFallback[K, DF, C]{
		Config:            Options(opts).Config(),
		PreOutput:         node.NewWithCustomDataFromKernel[C](ctx, &kernel.Passthrough{}),
		Output:            node.NewWithCustomDataFromKernel[C](ctx, &kernel.Passthrough{}),
		InputSwitch:       barrierstategetter.NewSwitch(),
		InputSyncer:       barrierstategetter.NewSwitch(),
		MonotonicPTS:      monotonicpts.New(true),
		newInputChainChan: make(chan *InputChain[K, DF, C], 100),
		Measurements: map[astiav.MediaType]*TrackMeasurements{
			astiav.MediaTypeVideo:   newTrackMeasurements(),
			astiav.MediaTypeAudio:   newTrackMeasurements(),
			astiav.MediaTypeUnknown: newTrackMeasurements(),
		},
	}
	i.PreOutput.AddPushTo(ctx, i.Output, packetorframefiltercondition.PacketOrFrame{i.MonotonicPTS})
	if err := i.initSwitches(ctx); err != nil {
		return nil, fmt.Errorf("cannot init switches: %w", err)
	}

	err := i.AddFactory(ctx, inputFactories...)
	if err != nil {
		return nil, fmt.Errorf("cannot add input factories: %w", err)
	}

	return i, nil
}

func (i *InputWithFallback[K, DF, C]) String() string {
	cur := i.InputSwitch.CurrentValue.Load()
	next := i.InputSwitch.NextValue.Load()
	ctx := context.Background()
	if !i.InputChainsLocker.ManualTryLock(ctx) {
		return fmt.Sprintf("InputWithFallback(<locked>; cur:%d, next:%d)", cur, next)
	}
	defer i.InputChainsLocker.ManualUnlock(ctx)
	var inputChainStrs []string
	for _, inputChain := range i.InputChains {
		isPaused := inputChain.IsPaused(ctx)
		var s []string
		s = append(s, inputChain.String())
		if int(inputChain.ID) == int(cur) {
			s = append(s, "current")
		}
		if int(inputChain.ID) == int(next) {
			s = append(s, "next")
		}
		if isPaused {
			s = append(s, "paused")
		}
		inputChainStrs = append(inputChainStrs, strings.Join(s, ":"))
	}
	return fmt.Sprintf("InputWithFallback(%s)", strings.Join(inputChainStrs, ", "))
}

func (i *InputWithFallback[K, DF, C]) GetOutput() node.Abstract {
	return i.Output
}

func (i *InputWithFallback[K, DF, C]) GetInputChainsCount(
	ctx context.Context,
) int {
	return xsync.DoR1(ctx, &i.InputChainsLocker, func() int {
		return len(i.InputChains)
	})
}

// PauseChain pauses the input chain at the given ID. Pausing all chains
// will suspend packet production until at least one chain is unpaused.
// Pausing an already-paused chain is a no-op.
func (i *InputWithFallback[K, DF, C]) PauseChain(
	ctx context.Context,
	id InputID,
) (_err error) {
	logger.Debugf(ctx, "PauseChain: %d", id)
	defer func() { logger.Debugf(ctx, "/PauseChain: %d: %v", id, _err) }()
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.pauseChainLocked, ctx, id)
}

func (i *InputWithFallback[K, DF, C]) pauseChainLocked(
	ctx context.Context,
	id InputID,
) error {
	chain := i.getInputChainByIDLocked(ctx, id)
	if chain == nil {
		return fmt.Errorf("input chain %d not found (have %d chains)", id, len(i.InputChains))
	}
	if chain.IsPaused(ctx) {
		return nil
	}

	activeCount := 0
	for _, c := range i.InputChains {
		if !c.IsPaused(ctx) {
			activeCount++
		}
	}
	if activeCount <= 1 {
		return ErrCannotPauseSoleActiveChain{ID: id}
	}

	return chain.Pause(ctx)
}

// UnpauseChain unpauses the input chain at the given ID.
func (i *InputWithFallback[K, DF, C]) UnpauseChain(
	ctx context.Context,
	id InputID,
) (_err error) {
	logger.Debugf(ctx, "UnpauseChain: %d", id)
	defer func() { logger.Debugf(ctx, "/UnpauseChain: %d: %v", id, _err) }()
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.unpauseChainLocked, ctx, id)
}

func (i *InputWithFallback[K, DF, C]) unpauseChainLocked(
	ctx context.Context,
	id InputID,
) error {
	chain := i.getInputChainByIDLocked(ctx, id)
	if chain == nil {
		return fmt.Errorf("input chain %d not found (have %d chains)", id, len(i.InputChains))
	}
	return chain.Unpause(ctx)
}

// releaseStaleSyncingCycle releases the OnBeforeSwitch → InputSyncer-
// KeepUnless cycle's switchingProcN reservation, if any, atomically
// clearing syncingGen. Returns true if a cycle was released.
//
// Used by:
//   - OnSwitchRequest: a fresh SetValue arrives while a prior cycle is
//     still pending — supersede it so the gate sees a clean state.
//   - OnInterruptedSwitch: commitToNextValue's CAS lost the race or a
//     setValueNow no-op switch — the OnBeforeSwitch we just paired with
//     never reaches the syncer, so release immediately.
//   - InputSyncer KeepUnless (via syncingGen.Swap(0) inline): the
//     normal completion path — KeepUnless first matched, sync done.
//
// The accounting is one-shot per cycle: only the first Swap(0) returns
// the active gen and decrements; concurrent late callers see 0 and
// no-op. A stale cycle's defer (one whose gen has been superseded by
// OnBeforeSwitch claiming a fresh gen) also no-ops via this path.
func (i *InputWithFallback[K, DF, C]) releaseStaleSyncingCycle() bool {
	if i.syncingGen.Swap(0) == 0 {
		return false
	}
	i.syncingSince.Store(time.Time{})
	i.switchingProcN.Add(-1)
	return true
}

func (i *InputWithFallback[K, DF, C]) initSwitches(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initSwitches")
	defer func() { logger.Debugf(ctx, "/initSwitches: %v", _err) }()

	i.InputSwitch.CurrentValue.Store(0)
	i.InputSyncer.CurrentValue.Store(0)

	// Intra-only allow-list mirrors streammux's OutputSwitch keep-unless via
	// the SSOT helper (codec/intra_only.go → IsIntraOnlyCodec). Pre-SSOT this
	// site listed only CodecIDRawvideo, so wrapped_avframe sources (lavfi /
	// testsrc carrier with AV_PKT_FLAG_KEY unset) silently failed to commit a
	// fallback switch while the streammux OutputSwitch accepted them — only
	// rawvideo demuxers like android_camera triggered both anchors.
	switchKeepUnlessConds := packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.Or{
			packetorframecondition.IsKeyFrame(true),
			packetorframecondition.IsIntraOnlyCodec{},
			packetorframecondition.AtomicBool(&i.AllowCorruptPackets),
		},
	}

	logger.Debugf(ctx, "Switch: setting keep-unless conditions: %s", switchKeepUnlessConds)
	i.InputSwitch.SetKeepUnless(switchKeepUnlessConds)

	i.InputSwitch.SetOnSwitchRequest(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		to int32,
	) (_err error) {
		logger.Debugf(ctx, "Switch.SetOnSwitchRequest: -> %d", to)
		defer func() { logger.Debugf(ctx, "/Switch.SetOnSwitchRequest: -> %d: %v", to, _err) }()
		// Supersede any stuck OnBeforeSwitch → InputSyncer-KeepUnless
		// cycle before gating: prevents the leak where a syncer that
		// never matched its predicate held switchingProcN above zero
		// indefinitely, rejecting all subsequent SetValue calls.
		if i.releaseStaleSyncingCycle() {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: superseded a stuck syncer cycle")
		}
		if v := i.switchingProcN.Add(1); v != 1 {
			i.switchingProcN.Add(-1)
			return ErrSwitchInProgress{ProcN: v - 1, To: to}
		}
		observability.Go(ctx, func(ctx context.Context) {
			defer i.switchingProcN.Add(-1)
			inputNext := i.getInputChainByID(ctx, InputID(to))
			if inputNext == nil {
				logger.Errorf(ctx, "Switch: target input %d not found", to)
				return
			}
			if err := inputNext.Unpause(ctx); err != nil {
				logger.Errorf(ctx, "Switch: unable to unpause the next input %d: %v", to, err)
			}
			// Unpause every intermediate chain in [0, to) too, so the
			// invariant `paused = (ID > CurrentValue)` holds for the
			// whole [0, to] range. Walk under InputChainsLocker so
			// concurrent AddFactory growth doesn't race the index
			// dereference.
			i.InputChainsLocker.Do(ctx, func() {
				for id := InputID(0); int32(id) < to; id++ {
					if int(id) >= len(i.InputChains) {
						break
					}
					mid := i.InputChains[id]
					if mid == nil || !mid.IsPaused(ctx) {
						continue
					}
					if err := mid.Unpause(ctx); err != nil {
						logger.Errorf(ctx, "Switch: unable to unpause intermediate input %d: %v", id, err)
					}
				}
			})
		})

		prevNext := i.InputSwitch.NextValue.Load()
		if prevNext == math.MinInt32 {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: no previous requested input")
			return nil
		}

		cur := i.InputSwitch.CurrentValue.Load()
		if prevNext <= cur {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: not pausing a higher priority input (than the currently active) %d <= %d", prevNext, cur)
			return nil
		}
		if prevNext == to {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: not pausing the same input %d", prevNext)
			return nil
		}

		logger.Debugf(ctx, "Switch.SetOnSwitchRequest: pausing previous requested input %d", prevNext)
		i.switchingProcN.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer i.switchingProcN.Add(-1)
			inputPrev := i.getInputChainByID(ctx, InputID(prevNext))
			if inputPrev == nil {
				logger.Errorf(ctx, "Switch: previous requested input %d not found", prevNext)
				return
			}
			if err := inputPrev.Pause(ctx); err != nil {
				logger.Errorf(ctx, "Switch: unable to pause the previous requested input %d: %v", prevNext, err)
			}
		})
		return nil
	})

	i.InputSwitch.SetOnBeforeSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "Switch.SetOnBeforeSwitch: %d -> %d", from, to)
		// Claim a fresh syncing generation. If a prior cycle's gen was
		// still live, this Swap supersedes it: the prior cycle's
		// teardown sites will Swap(0) and see a non-matching value
		// (0 or our newGen), so they no-op — and we inherit the prior
		// reservation rather than double-counting.
		newGen := i.nextSyncingGen.Add(1)
		if i.syncingGen.Swap(newGen) == 0 {
			i.switchingProcN.Add(1)
		}
	})

	i.InputSwitch.SetOnInterruptedSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "Switch.SetOnInterruptedSwitch: %d -> %d", from, to)
		// Release the reservation taken by the paired OnBeforeSwitch.
		// Swap(0) is one-shot: a concurrent supersession by a fresh
		// OnBeforeSwitch already changed syncingGen, so we no-op and
		// the new cycle owns the live reservation.
		i.releaseStaleSyncingCycle()
	})

	i.InputSwitch.SetOnAfterSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		if v := in.Get(); v != nil {
			ctx = belt.WithField(ctx, "media_type", v.GetMediaType().String())
		} else {
			logger.Warnf(ctx, "Switch.SetOnAfterSwitch: no packet/frame for %d -> %d", from, to)
		}
		logger.Debugf(ctx, "Switch.SetOnAfterSwitch: %d -> %d", from, to)

		assert(ctx, i.syncingSince.Load().IsZero(), "syncingSince must be zero")

		i.syncingSince.Store(time.Now())
		if in.Get() != nil {
			in.AddPipelineSideData(kernel.SideFlagFlush{})
		}

		for inputID := from; inputID > to; inputID-- {
			inputID := inputID
			i.switchingProcN.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer i.switchingProcN.Add(-1)
				inputPrev := i.getInputChainByID(ctx, InputID(inputID))
				if inputPrev == nil {
					logger.Errorf(ctx, "Switch: previous input %d not found", inputID)
					return
				}
				if err := inputPrev.Pause(ctx); err != nil {
					logger.Errorf(ctx, "Switch: unable to pause the previous input %d: %v", inputID, err)
				}
			})
		}

		logger.Debugf(ctx, "Syncer.SetValue(ctx, %d): from %d", to, from)
		err := i.InputSyncer.SetValue(ctx, to)
		logger.Debugf(ctx, "/Syncer.SetValue(ctx, %d): from %d: %v", to, from, err)
	})
	i.InputSyncer.SetKeepUnless(packetorframecondition.Function(func(
		ctx context.Context,
		in packetorframe.InputUnion,
	) (_ret bool) {
		defer func() {
			if _ret {
				// Tag-driven release: only the active cycle's
				// completion decrements switchingProcN. A defer
				// belonging to a superseded cycle would race here
				// and Swap(0) returns 0 → no-op.
				i.releaseStaleSyncingCycle()
			}
		}()
		if in.GetPipelineSideData().Contains(kernel.SideFlagFlush{}) {
			return true
		}
		if time.Since(i.syncingSince.Load()) <= i.Config.SwitchKeepUnlessTimeout {
			return false
		}
		logger.Errorf(ctx, "Syncer: switching took too long")
		if in.Frame != nil {
			return true
		}
		// not decoded, we have to wait for a keyframe on the video track:
		if in.GetMediaType() != astiav.MediaTypeVideo {
			return false
		}
		if in.GetCodecParameters() != nil {
			logger.Debugf(ctx, "Syncer keep-unless: media=video key=%t codec_id=%s", in.IsKey(), in.GetCodecParameters().CodecID())
		}
		if in.IsKey() {
			return true
		}
		// Intra-only release: SSOT in codec/intra_only.go covers both
		// rawvideo (android_camera, v4l2) and wrapped_avframe (lavfi /
		// test sources). Pre-SSOT this site listed only rawvideo, which
		// matched the InputSwitch keep-unless's parallel divergence —
		// see the SSOT comment on switchKeepUnlessConds above.
		if cp := in.GetCodecParameters(); cp != nil && codec.IsIntraOnlyCodec(cp.CodecID()) {
			return true
		}
		return false
	}))
	i.InputSyncer.Flags.Set(0 |
		barrierstategetter.SwitchFlagNextOutputStateBlock,
	)

	logger.Tracef(ctx, "Switch: %p", i.InputSwitch)
	logger.Tracef(ctx, "Syncer: %p", i.InputSyncer)
	return nil
}

func (i *InputWithFallback[K, DF, C]) getInputChainByID(
	ctx context.Context,
	id InputID,
) *InputChain[K, DF, C] {
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.getInputChainByIDLocked, ctx, id)
}

func (i *InputWithFallback[K, DF, C]) getInputChainByIDLocked(
	_ context.Context,
	id InputID,
) *InputChain[K, DF, C] {
	if int(id) < 0 || int(id) >= len(i.InputChains) {
		return nil
	}
	return i.InputChains[id]
}

func (i *InputWithFallback[K, DF, C]) AddFactory(
	ctx context.Context,
	inputFactories ...InputFactory[K, DF, C],
) (_err error) {
	logger.Debugf(ctx, "AddFactory")
	defer func() { logger.Debugf(ctx, "/AddFactory: %v", _err) }()
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.addFactory, ctx, inputFactories)
}

func (i *InputWithFallback[K, DF, C]) addFactory(
	ctx context.Context,
	inputFactories []InputFactory[K, DF, C],
) error {
	for _, inputFactory := range inputFactories {
		inputID := InputID(len(i.InputChains))
		inputChain, err := newInputChain(ctx,
			inputID, inputFactory,
			i.InputSwitch.Output(int32(inputID)),
			i.InputSyncer.Output(int32(inputID)),
			i.Config.QuietOnOpenFailure,
			i.Config.ResetDownstreamKernelsTimeout,
			i.onInputChainKernelOpen,
			i.onInputChainError,
		)
		if err != nil {
			return fmt.Errorf("cannot create input chain for input %d: %w", inputID, err)
		}
		// Attach the per-instance InputFilter to inputChain.Filter (a real
		// destination node that receives pre-decode packets pushed from
		// inputChain.Input). Setting the filter on inputChain.Input itself
		// would be dead code: Input is a source node, so no node ever pushes
		// to it and its GetInputFilter is never consulted.
		node.AppendInputFilter(ctx, inputChain.Filter, i.inputFilter())
		inputChain.GetOutput().AddPushTo(ctx, i.PreOutput)
		i.InputChains = append(i.InputChains, inputChain)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case i.newInputChainChan <- inputChain:
		default:
			if err := inputChain.Close(ctx); err != nil {
				logger.Errorf(ctx, "unable to close input chain: %v", err)
			}
			return fmt.Errorf("cannot send new input chain to the init queue: it is already full")
		}
	}
	return nil
}

func (i *InputWithFallback[K, DF, C]) onInputChainKernelOpen(
	ctx context.Context,
	inputChain *InputChain[K, DF, C],
) {
	// When a kernel opens, prefer it if there is no active input or it has higher priority
	id := int(inputChain.ID)
	cur := int(i.InputSwitch.CurrentValue.Load())
	logger.Debugf(ctx, "onInputChainKernelOpen: input %d opened, current=%d", id, cur)
	if id >= cur {
		return
	}

	// If this input has a higher priority (lower index), request a switch back
	if err := i.InputSwitch.SetValue(ctx, int32(id)); err != nil {
		logger.Errorf(ctx, "onInputChainKernelOpen: unable to recover to input %d: %v", id, err)
	}
}

func (i *InputWithFallback[K, DF, C]) onInputChainError(
	ctx context.Context,
	inputChain *InputChain[K, DF, C],
	err error,
) (_err error) {
	logger.Debugf(ctx, "onInputChainError: input %d error: %v", int(inputChain.ID), err)
	defer func() {
		logger.Debugf(ctx, "/onInputChainError: input %d error: %v: %v", int(inputChain.ID), err, _err)
	}()

	if i.Config.RetryInterval < 0 {
		return fmt.Errorf("retries are disabled, and input %d errored: %w", int(inputChain.ID), err)
	}

	defer time.Sleep(i.Config.RetryInterval)

	id := inputChain.ID
	active := int(i.InputSwitch.CurrentValue.Load())
	next := int(i.InputSwitch.NextValue.Load())
	current := max(active, next)
	logger.Debugf(ctx, "onInputChainError: current:%d", current)

	// Only react to errors on the currently active input
	if current != int(id) {
		return nil
	}

	i.InputChainsLocker.Do(ctx, func() {
		keepUnlessSwitch := i.InputSwitch.GetKeepUnless()
		if keepUnlessSwitch != nil {
			i.InputSwitch.SetKeepUnless(nil)
			defer i.InputSwitch.SetKeepUnless(keepUnlessSwitch)
		}
		keepUnlessSyncer := i.InputSyncer.GetKeepUnless()
		if keepUnlessSyncer != nil {
			i.InputSyncer.SetKeepUnless(nil)
			defer i.InputSyncer.SetKeepUnless(keepUnlessSyncer)
		}

		// Choose the next fallback via the SSOT WalkAvailableAfter
		// helper. Skips chains whose factory implements
		// InputFactoryWithAvailability and reports no resources. This
		// avoids the procN latch race on sparse chain layouts (the
		// dense-walk pre-fix issued one switch per empty chain, and
		// the next chain's onInputChainError raced against the
		// in-progress latch with "another switch is in progress").
		// The same helper backs ffstream.RemoveInput's removal-driven
		// fallback walk, so a future factory adding custom
		// availability semantics behaves consistently across both
		// trigger paths.
		nextID := InputID(WalkAvailableAfter(ctx, i.InputChains, int(id)))
		if nextID < 0 {
			logger.Debugf(ctx, "onInputChainError: no fallbacks available past %d (have %d chains)", int(id), len(i.InputChains))
			return
		}
		logger.Infof(ctx, "onInputChainError: switching from %d to %d due to error: %v", int(id), nextID, err)
		if switchErr := i.InputSwitch.SetValue(ctx, int32(nextID)); switchErr != nil {
			// Demote the cascading "another switch is in progress"
			// startup-walk noise to Debug when QuietOnOpenFailure is
			// enabled. The fallback walk across consecutive empty
			// slots races itself on the procN latch every retry tick;
			// at startup (before any priority is provisioned) this is
			// by-design. Other SetValue failures keep Errorf so real
			// switch contention remains visible.
			if i.Config.QuietOnOpenFailure && errors.Is(switchErr, ErrSwitchInProgress{}) {
				logger.Debugf(ctx, "onInputChainError: switch to fallback %d superseded by in-flight switch: %v", nextID, switchErr)
			} else {
				logger.Errorf(ctx, "onInputChainError: unable to switch to fallback %d: %v", nextID, switchErr)
			}
		}
	})
	return nil
}

type asInputFilter[K InputKernel, DF codec.DecoderFactory, C any] InputWithFallback[K, DF, C]

func (f *asInputFilter[K, DF, C]) String() string {
	return "InputWithFallback:InputFilter"
}

func (f *asInputFilter[K, DF, C]) Match(
	ctx context.Context,
	in packetorframefiltercondition.Input,
) bool {
	v := f.InputFilter.Load()
	if v == nil {
		return true
	}
	return v.Match(ctx, in)
}

func (i *InputWithFallback[K, DF, C]) inputFilter() packetorframefiltercondition.Condition {
	return (*asInputFilter[K, DF, C])(i)
}

func (i *InputWithFallback[K, DF, C]) GetInputs(
	ctx context.Context,
) InputNodes[K, C] {
	return xsync.DoR1(ctx, &i.InputChainsLocker, i.getInputsLocked)
}

func (i *InputWithFallback[K, DF, C]) getInputsLocked() InputNodes[K, C] {
	inputs := make([]*InputNode[K, C], 0, len(i.InputChains))
	for _, inputChain := range i.InputChains {
		inputs = append(inputs, inputChain.Input)
	}
	return inputs
}

type TrackMeasurements struct {
	InputBitRate  atomic.Uint64
	OutputBitRate atomic.Uint64
}

func newTrackMeasurements() *TrackMeasurements {
	return &TrackMeasurements{}
}

func (i *InputWithFallback[K, DF, C]) getTrackMeasurements(mediaType astiav.MediaType) *TrackMeasurements {
	m := i.Measurements[mediaType]
	if m != nil {
		return m
	}
	return i.Measurements[astiav.MediaTypeUnknown]
}

func updateWithInertialValue(
	oldValue uint64,
	newValue uint64,
	inertia float64,
	measurementsCount uint64,
) uint64 {
	if measurementsCount == 0 {
		return newValue
	}
	return uint64(float64(oldValue)*inertia + float64(newValue)*(1.0-inertia))
}

func (i *InputWithFallback[K, DF, C]) inputBitRateMeasurerLoop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "inputBitRateMeasurerLoop")
	defer func() { logger.Tracef(ctx, "/inputBitRateMeasurerLoop: %v", _err) }()

	t := time.NewTicker(time.Second / 4)
	defer t.Stop()
	bytesInputReadPrev := map[astiav.MediaType]uint64{}
	bytesOutputReadPrev := map[astiav.MediaType]uint64{}
	tsPrev := time.Now()
	for {
		var tsNext time.Time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tsNext = <-t.C:
			duration := tsNext.Sub(tsPrev)

			bytesInputReadNext := map[astiav.MediaType]uint64{}
			i.InputChainsLocker.Do(ctx, func() {
				for _, inputChain := range i.InputChains {
					inputCounters := inputChain.Input.GetCountersPtr()
					bytesInputReadNext[astiav.MediaTypeVideo] += inputCounters.Sent.Packets.Video.Bytes.Load() + inputCounters.Sent.Frames.Video.Bytes.Load()
					bytesInputReadNext[astiav.MediaTypeAudio] += inputCounters.Sent.Packets.Audio.Bytes.Load() + inputCounters.Sent.Frames.Audio.Bytes.Load()
					bytesInputReadNext[astiav.MediaTypeUnknown] += inputCounters.Sent.Packets.Other.Bytes.Load() + inputCounters.Sent.Frames.Other.Bytes.Load()
				}
			})

			outputCounters := i.Output.GetCountersPtr()
			bytesOutputReadNext := map[astiav.MediaType]uint64{
				astiav.MediaTypeVideo:   outputCounters.Received.Packets.Video.Bytes.Load() + outputCounters.Received.Frames.Video.Bytes.Load(),
				astiav.MediaTypeAudio:   outputCounters.Received.Packets.Audio.Bytes.Load() + outputCounters.Received.Frames.Audio.Bytes.Load(),
				astiav.MediaTypeUnknown: outputCounters.Received.Packets.Other.Bytes.Load() + outputCounters.Received.Frames.Other.Bytes.Load(),
			}

			for _, mediaType := range []astiav.MediaType{astiav.MediaTypeVideo, astiav.MediaTypeAudio, astiav.MediaTypeUnknown} {
				m := i.getTrackMeasurements(mediaType)
				bytesInputRead := uint64(0)
				if bytesInputReadNext[mediaType] >= bytesInputReadPrev[mediaType] {
					bytesInputRead = bytesInputReadNext[mediaType] - bytesInputReadPrev[mediaType]
				}
				bitRateInput := int(float64(bytesInputRead*8) / duration.Seconds())
				oldInputValue := m.InputBitRate.Load()
				newInputValue := updateWithInertialValue(oldInputValue, uint64(bitRateInput), 0.9, i.CurrentBitRateMeasurementsCount.Load())
				m.InputBitRate.Store(newInputValue)

				bytesOutputRead := uint64(0)
				if bytesOutputReadNext[mediaType] >= bytesOutputReadPrev[mediaType] {
					bytesOutputRead = bytesOutputReadNext[mediaType] - bytesOutputReadPrev[mediaType]
				}
				bitRateOutput := int(float64(bytesOutputRead*8) / duration.Seconds())
				oldOutputValue := m.OutputBitRate.Load()
				newOutputValue := updateWithInertialValue(oldOutputValue, uint64(bitRateOutput), 0.9, i.CurrentBitRateMeasurementsCount.Load())
				m.OutputBitRate.Store(newOutputValue)

				logger.Tracef(ctx, "inputBitRateMeasurerLoop: mediaType:%v, duration:%v, bytesInputRead:%v, bitRateInput:%v, oldInputBitRate:%v, newInputBitRate:%v, bytesOutputRead:%v, bitRateOutput:%v, oldOutputBitRate:%v, newOutputBitRate:%v (raw: inputNext:%v, inputPrev:%v, outputNext:%v, outputPrev:%v)", mediaType, duration, bytesInputRead, bitRateInput, oldInputValue, newInputValue, bytesOutputRead, bitRateOutput, oldOutputValue, newOutputValue, bytesInputReadNext[mediaType], bytesInputReadPrev[mediaType], bytesOutputReadNext[mediaType], bytesOutputReadPrev[mediaType])
			}

			bytesInputReadPrev = bytesInputReadNext
			bytesOutputReadPrev = bytesOutputReadNext
			tsPrev = tsNext
			i.CurrentBitRateMeasurementsCount.Add(1)
		}
	}
}

type BitRates struct {
	Input  globaltypes.BitRateInfo
	Output globaltypes.BitRateInfo
}

func (i *InputWithFallback[K, DF, C]) GetBitRates(
	ctx context.Context,
) *BitRates {
	video := i.getTrackMeasurements(astiav.MediaTypeVideo)
	audio := i.getTrackMeasurements(astiav.MediaTypeAudio)
	other := i.getTrackMeasurements(astiav.MediaTypeUnknown)
	return &BitRates{
		Input: globaltypes.BitRateInfo{
			Video: globaltypes.Ubps(video.InputBitRate.Load()),
			Audio: globaltypes.Ubps(audio.InputBitRate.Load()),
			Other: globaltypes.Ubps(other.InputBitRate.Load()),
		},
		Output: globaltypes.BitRateInfo{
			Video: globaltypes.Ubps(video.OutputBitRate.Load()),
			Audio: globaltypes.Ubps(audio.OutputBitRate.Load()),
			Other: globaltypes.Ubps(other.OutputBitRate.Load()),
		},
	}
}
