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
	selectorfanin "github.com/xaionaro-go/avpipeline/preset/selector/fanin"
	selectorid "github.com/xaionaro-go/avpipeline/preset/selector/id"
	selectormember "github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchprogress"
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
	// switchGate owns the selector-compatible in-flight switch
	// accounting. It tracks OnSwitchRequest work and the
	// OnBeforeSwitch -> InputSyncer-KeepUnless cycle so a fresh switch
	// can supersede a stuck syncer cycle without process restart.
	switchGate switchprogress.Gate

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

type chainLifecycleOperation uint8

const (
	chainLifecyclePause chainLifecycleOperation = iota + 1
	chainLifecycleUnpause
)

type switchLifecyclePlan[K InputKernel, DF codec.DecoderFactory, C any] struct {
	UnpauseBeforeSwitch  []*InputChain[K, DF, C]
	PauseAfterSwitch     []*InputChain[K, DF, C]
	PausePreviousPending []*InputChain[K, DF, C]
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
	chains, err := xsync.DoA2R2(ctx, &i.InputChainsLocker, i.planPauseChainLocked, ctx, id)
	if err != nil {
		return err
	}
	return runChainLifecycle(ctx, chains, chainLifecyclePause)
}

func (i *InputWithFallback[K, DF, C]) planPauseChainLocked(
	ctx context.Context,
	id InputID,
) ([]*InputChain[K, DF, C], error) {
	chain := i.getInputChainByIDLocked(ctx, id)
	if chain == nil {
		return nil, fmt.Errorf("input chain %d not found (have %d chains)", id, len(i.InputChains))
	}

	state, chainsByID := i.pauseStateLocked(
		ctx,
		selectorid.MemberID(i.InputSwitch.CurrentValue.Load()),
		selectorid.MemberID(i.InputSwitch.NextValue.Load()),
		selectorid.MemberID(id),
	)
	planner := selectorfanin.PausePlanner[InputID, *InputChain[K, DF, C]]{}
	plan, err := planner.PlanPause(ctx, state, selectorid.MemberID(id))
	if errors.Is(err, selectorfanin.ErrCannotPauseSoleActiveMember) {
		return nil, ErrCannotPauseSoleActiveChain{ID: id}
	}
	if err != nil {
		return nil, err
	}

	return chainsByMemberIDs(chainsByID, plan.PauseAfterSwitch), nil
}

// UnpauseChain unpauses the input chain at the given ID.
func (i *InputWithFallback[K, DF, C]) UnpauseChain(
	ctx context.Context,
	id InputID,
) (_err error) {
	logger.Debugf(ctx, "UnpauseChain: %d", id)
	defer func() { logger.Debugf(ctx, "/UnpauseChain: %d: %v", id, _err) }()
	chains, err := xsync.DoA2R2(ctx, &i.InputChainsLocker, i.planUnpauseChainLocked, ctx, id)
	if err != nil {
		return err
	}
	return runChainLifecycle(ctx, chains, chainLifecycleUnpause)
}

func (i *InputWithFallback[K, DF, C]) planUnpauseChainLocked(
	ctx context.Context,
	id InputID,
) ([]*InputChain[K, DF, C], error) {
	chain := i.getInputChainByIDLocked(ctx, id)
	if chain == nil {
		return nil, fmt.Errorf("input chain %d not found (have %d chains)", id, len(i.InputChains))
	}

	state, chainsByID := i.pauseStateLocked(
		ctx,
		selectorid.MemberID(i.InputSwitch.CurrentValue.Load()),
		selectorid.MemberID(i.InputSwitch.NextValue.Load()),
		selectorid.MemberID(id),
	)
	planner := selectorfanin.PausePlanner[InputID, *InputChain[K, DF, C]]{}
	plan, err := planner.PlanUnpause(ctx, state, selectorid.MemberID(id))
	if err != nil {
		return nil, err
	}

	return chainsByMemberIDs(chainsByID, plan.UnpauseBeforeSwitch), nil
}

func (i *InputWithFallback[K, DF, C]) pauseStateLocked(
	ctx context.Context,
	current selectorid.MemberID,
	previousPending selectorid.MemberID,
	target selectorid.MemberID,
) (selectorfanin.PauseState, map[selectorid.MemberID]*InputChain[K, DF, C]) {
	state := selectorfanin.PauseState{
		Current:         current,
		PreviousPending: previousPending,
		Target:          target,
		Paused:          map[selectorid.MemberID]bool{},
	}
	chainsByID := map[selectorid.MemberID]*InputChain[K, DF, C]{}
	for _, chain := range i.InputChains {
		if chain == nil {
			continue
		}
		memberID := selectorid.MemberID(chain.ID)
		paused := chain.IsPaused(ctx)
		state.Priorities = append(state.Priorities, memberID)
		state.Paused[memberID] = paused
		chainsByID[memberID] = chain
		if !paused {
			state.UnpausedCount++
		}
	}
	return state, chainsByID
}

func chainsByMemberIDs[K InputKernel, DF codec.DecoderFactory, C any](
	chainsByID map[selectorid.MemberID]*InputChain[K, DF, C],
	memberIDs []selectorid.MemberID,
) []*InputChain[K, DF, C] {
	chains := make([]*InputChain[K, DF, C], 0, len(memberIDs))
	for _, memberID := range memberIDs {
		chain, ok := chainsByID[memberID]
		if !ok {
			continue
		}
		chains = append(chains, chain)
	}
	return chains
}

func runChainLifecycle[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	chains []*InputChain[K, DF, C],
	operation chainLifecycleOperation,
) error {
	var errs []error
	for _, chain := range chains {
		if chain == nil {
			continue
		}
		var err error
		switch operation {
		case chainLifecyclePause:
			err = chain.Pause(ctx)
		case chainLifecycleUnpause:
			err = chain.Unpause(ctx)
		default:
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("input chain %d lifecycle operation %d failed: %w", chain.ID, operation, err))
		}
	}
	return errors.Join(errs...)
}

func (i *InputWithFallback[K, DF, C]) planSwitchLifecycle(
	ctx context.Context,
	target InputID,
	current int32,
	previousPending int32,
) (switchLifecyclePlan[K, DF, C], error) {
	i.InputChainsLocker.ManualLock(ctx)
	defer i.InputChainsLocker.ManualUnlock(ctx)
	return i.planSwitchLifecycleLocked(
		ctx,
		target,
		selectorid.MemberID(current),
		selectorid.MemberID(previousPending),
	)
}

func (i *InputWithFallback[K, DF, C]) planSwitchLifecycleLocked(
	ctx context.Context,
	target InputID,
	current selectorid.MemberID,
	previousPending selectorid.MemberID,
) (switchLifecyclePlan[K, DF, C], error) {
	state, chainsByID := i.pauseStateLocked(ctx, current, previousPending, selectorid.MemberID(target))
	planner := selectorfanin.PausePlanner[InputID, *InputChain[K, DF, C]]{}
	plan, err := planner.PlanSwitch(ctx, state)
	if err != nil {
		return switchLifecyclePlan[K, DF, C]{}, err
	}

	return switchLifecyclePlan[K, DF, C]{
		UnpauseBeforeSwitch:  chainsByMemberIDs(chainsByID, plan.UnpauseBeforeSwitch),
		PauseAfterSwitch:     chainsByMemberIDs(chainsByID, plan.PauseAfterSwitch),
		PausePreviousPending: chainsByMemberIDs(chainsByID, plan.PausePreviousPending),
	}, nil
}

// releaseStaleSyncingCycle releases the OnBeforeSwitch -> InputSyncer-
// KeepUnless cycle's selector gate reservation, if any. Returns true
// if a cycle was released.
//
// Used by:
//   - OnSwitchRequest: a fresh SetValue arrives while a prior cycle is
//     still pending — supersede it so the gate sees a clean state.
//   - OnInterruptedSwitch: commitToNextValue's CAS lost the race or a
//     setValueNow no-op switch — the OnBeforeSwitch we just paired with
//     never reaches the syncer, so release immediately.
//   - InputSyncer KeepUnless: the normal completion path — KeepUnless
//     first matched, sync done.
//
// The accounting is one-shot per cycle inside switchprogress.Gate.
// Concurrent late callers see no active cycle and no-op.
func (i *InputWithFallback[K, DF, C]) releaseStaleSyncingCycle() bool {
	if !i.switchGate.SupersedeStuckCycle() {
		return false
	}
	i.syncingSince.Store(time.Time{})
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
		// never matched its predicate held the switch gate above zero
		// indefinitely, rejecting all subsequent SetValue calls.
		if i.releaseStaleSyncingCycle() {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: superseded a stuck syncer cycle")
		}
		work, err := i.switchGate.StartRequest(selectorid.MemberID(to))
		if err != nil {
			var inProgress switchprogress.ErrSwitchInProgress
			if errors.As(err, &inProgress) {
				return ErrSwitchInProgress{ProcN: inProgress.ProcN, To: to}
			}
			return err
		}
		defer work.Release()

		prevNext := i.InputSwitch.NextValue.Load()
		cur := i.InputSwitch.CurrentValue.Load()
		plan, err := i.planSwitchLifecycle(ctx, InputID(to), cur, prevNext)
		if err != nil {
			return err
		}

		if len(plan.UnpauseBeforeSwitch) == 0 && i.getInputChainByID(ctx, InputID(to)) == nil {
			logger.Errorf(ctx, "Switch: target input %d not found", to)
		}
		if len(plan.UnpauseBeforeSwitch) > 0 {
			release := work.ReserveAsyncWork()
			observability.Go(ctx, func(ctx context.Context) {
				defer release()
				if err := runChainLifecycle(ctx, plan.UnpauseBeforeSwitch, chainLifecycleUnpause); err != nil {
					logger.Errorf(ctx, "Switch: unable to unpause input chain(s) before switching to %d: %v", to, err)
				}
			})
		}

		if prevNext == math.MinInt32 {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: no previous requested input")
			return nil
		}

		if prevNext <= cur {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: not pausing a higher priority input (than the currently active) %d <= %d", prevNext, cur)
			return nil
		}
		if prevNext == to {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: not pausing the same input %d", prevNext)
			return nil
		}

		logger.Debugf(ctx, "Switch.SetOnSwitchRequest: pausing previous requested input %d", prevNext)
		if len(plan.PausePreviousPending) == 0 && i.getInputChainByID(ctx, InputID(prevNext)) == nil {
			logger.Errorf(ctx, "Switch: previous requested input %d not found", prevNext)
			return nil
		}
		if len(plan.PausePreviousPending) > 0 {
			release := work.ReserveAsyncWork()
			observability.Go(ctx, func(ctx context.Context) {
				defer release()
				if err := runChainLifecycle(ctx, plan.PausePreviousPending, chainLifecyclePause); err != nil {
					logger.Errorf(ctx, "Switch: unable to pause the previous requested input %d: %v", prevNext, err)
				}
			})
		}
		return nil
	})

	i.InputSwitch.SetOnBeforeSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "Switch.SetOnBeforeSwitch: %d -> %d", from, to)
		i.switchGate.BeginSyncerCycle()
	})

	i.InputSwitch.SetOnInterruptedSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "Switch.SetOnInterruptedSwitch: %d -> %d", from, to)
		// Release the reservation taken by the paired OnBeforeSwitch.
		// Swap(0) is one-shot: a concurrent supersession by a fresh
		// OnBeforeSwitch already superseded the active cycle, so we
		// no-op and the new cycle owns the live reservation.
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

		plan, err := i.planSwitchLifecycle(ctx, InputID(to), from, i.InputSwitch.NextValue.Load())
		if err != nil {
			logger.Errorf(ctx, "Switch: unable to plan post-switch pauses for %d -> %d: %v", from, to, err)
		}
		if err := runChainLifecycle(ctx, plan.PauseAfterSwitch, chainLifecyclePause); err != nil {
			logger.Errorf(ctx, "Switch: unable to pause previous input chain(s) after switching to %d: %v", to, err)
		}

		logger.Debugf(ctx, "Syncer.SetValue(ctx, %d): from %d", to, from)
		syncerErr := i.InputSyncer.SetValue(ctx, to)
		logger.Debugf(ctx, "/Syncer.SetValue(ctx, %d): from %d: %v", to, from, syncerErr)
	})
	i.InputSyncer.SetKeepUnless(packetorframecondition.Function(func(
		ctx context.Context,
		in packetorframe.InputUnion,
	) (_ret bool) {
		defer func() {
			if _ret {
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
	var closeAfterUnlock []*InputChain[K, DF, C]
	i.InputChainsLocker.ManualLock(ctx)
	err := i.addFactory(ctx, inputFactories, &closeAfterUnlock)
	i.InputChainsLocker.ManualUnlock(ctx)
	closeErr := closeInputChains(ctx, closeAfterUnlock)
	return errors.Join(err, closeErr)
}

func (i *InputWithFallback[K, DF, C]) addFactory(
	ctx context.Context,
	inputFactories []InputFactory[K, DF, C],
	closeAfterUnlock *[]*InputChain[K, DF, C],
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
			i.InputChains = i.InputChains[:len(i.InputChains)-1]
			*closeAfterUnlock = append(*closeAfterUnlock, inputChain)
			return ctx.Err()
		case i.newInputChainChan <- inputChain:
		default:
			i.InputChains = i.InputChains[:len(i.InputChains)-1]
			*closeAfterUnlock = append(*closeAfterUnlock, inputChain)
			return fmt.Errorf("cannot send new input chain to the init queue: it is already full")
		}
	}
	return nil
}

func closeInputChains[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	chains []*InputChain[K, DF, C],
) error {
	var errs []error
	for _, inputChain := range chains {
		if inputChain == nil {
			continue
		}
		if err := inputChain.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close input chain: %v", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

	nextID, ok, planErr := i.planFallbackAfterFailure(ctx, inputChain, InputID(current), err)
	if planErr != nil {
		return planErr
	}
	if !ok {
		logger.Debugf(ctx, "onInputChainError: no fallbacks available past %d (have %d chains)", int(id), i.GetInputChainsCount(ctx))
		return nil
	}

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

	logger.Infof(ctx, "onInputChainError: switching from %d to %d due to error: %v", int(id), nextID, err)
	if switchErr := i.InputSwitch.SetValue(ctx, int32(nextID)); switchErr != nil {
		// Demote the cascading "another switch is in progress"
		// startup-walk noise to Debug when QuietOnOpenFailure is
		// enabled. The fallback walk across consecutive empty slots
		// races itself on the selector switch-progress gate every
		// retry tick; at startup (before any priority is provisioned)
		// this is by-design. Other SetValue failures keep Errorf so
		// real switch contention remains visible.
		if i.Config.QuietOnOpenFailure && errors.Is(switchErr, ErrSwitchInProgress{}) {
			logger.Debugf(ctx, "onInputChainError: switch to fallback %d superseded by in-flight switch: %v", nextID, switchErr)
		} else {
			logger.Errorf(ctx, "onInputChainError: unable to switch to fallback %d: %v", nextID, switchErr)
		}
	}
	return nil
}

func (i *InputWithFallback[K, DF, C]) planFallbackAfterFailure(
	ctx context.Context,
	inputChain *InputChain[K, DF, C],
	current InputID,
	cause error,
) (InputID, bool, error) {
	i.InputChainsLocker.ManualLock(ctx)
	defer i.InputChainsLocker.ManualUnlock(ctx)

	handler := selectorfanin.FallbackHandler[InputID, *InputChain[K, DF, C]]{}
	decision, err := handler.HandleFailure(
		ctx,
		selectorfanin.Failure[InputID]{
			MemberID:   selectorid.MemberID(inputChain.ID),
			StorageKey: inputChain.ID,
			Current:    selectorid.MemberID(current),
			Next:       selectorid.MemberID(current),
			Cause:      cause,
		},
		i.fallbackCandidatesLocked(),
	)
	if err != nil {
		return 0, false, err
	}
	if decision.IgnoreFailure || decision.UseRecreate {
		return 0, false, nil
	}
	return InputID(decision.SwitchTo), true, nil
}

func (i *InputWithFallback[K, DF, C]) fallbackCandidatesLocked() []selectorfanin.PriorityCandidate[InputID, *InputChain[K, DF, C]] {
	candidates := make([]selectorfanin.PriorityCandidate[InputID, *InputChain[K, DF, C]], 0, len(i.InputChains))
	for _, chain := range i.InputChains {
		if chain == nil {
			continue
		}
		memberID := selectorid.MemberID(chain.ID)
		candidates = append(candidates, selectorfanin.PriorityCandidate[InputID, *InputChain[K, DF, C]]{
			Priority: memberID,
			Entry: selectormember.Entry[InputID, *InputChain[K, DF, C]]{
				ID:         memberID,
				StorageKey: chain.ID,
				Value:      chain,
			},
			Availability: availabilityCandidate(chain),
		})
	}
	return candidates
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
