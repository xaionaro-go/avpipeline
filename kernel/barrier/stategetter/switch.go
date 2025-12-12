package stategetter

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/xsync"
)

type FuncOnSwitchRequest func(ctx context.Context, pkt packetorframe.InputUnion, to int32) error
type FuncOnBeforeSwitch func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32)
type FuncOnInterruptedSwitch func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32)
type FuncOnAfterSwitch func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32)

type SwitchFlags = types.SwitchFlags

const (
	SwitchFlagFirstPacketAfterSwitchPassBothOutputs = types.SwitchFlagFirstPacketAfterSwitchPassBothOutputs
	SwitchFlagForbidTakeoverInKeepUnless            = types.SwitchFlagForbidTakeoverInKeepUnless
	SwitchFlagNextOutputStateBlock                  = types.SwitchFlagNextOutputStateBlock
	SwitchFlagInactiveBlock                         = types.SwitchFlagInactiveBlock
)

type State = types.State

const (
	StatePass  State = types.StatePass
	StateDrop  State = types.StateDrop
	StateBlock State = types.StateBlock
)

type Switch struct {
	CurrentValue  atomic.Int32
	NextValue     atomic.Int32
	PreviousValue atomic.Int32

	ChangeSignal                  *chan struct{}
	KeepUnlessPacketCond          *packetorframecondition.Condition
	OnSwitchRequest               *FuncOnSwitchRequest
	OnBeforeSwitch                *FuncOnBeforeSwitch
	OnInterruptedSwitch           *FuncOnInterruptedSwitch
	OnAfterSwitch                 *FuncOnAfterSwitch
	Flags                         SwitchFlags
	FirstPacketOrFrameAfterSwitch packetorframe.Abstract

	CommitMutex xsync.Mutex
}

func NewSwitch() *Switch {
	sw := &Switch{
		ChangeSignal: ptr(make(chan struct{})),
	}
	sw.NextValue.Store(math.MinInt32)
	sw.PreviousValue.Store(math.MinInt32)
	return sw
}

var _ StateGetter = (*Switch)(nil).Output(0)

func (s *Switch) GetKeepUnless() packetorframecondition.Condition {
	ptr := xatomic.LoadPointer(&s.KeepUnlessPacketCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetKeepUnless(cond packetorframecondition.Condition) {
	xatomic.StorePointer(&s.KeepUnlessPacketCond, ptr(cond))
}

func (s *Switch) GetOnSwitchRequest() FuncOnSwitchRequest {
	ptr := xatomic.LoadPointer(&s.OnSwitchRequest)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetOnSwitchRequest(fn FuncOnSwitchRequest) {
	xatomic.StorePointer(&s.OnSwitchRequest, ptr(fn))
}

func (s *Switch) GetOnBeforeSwitch() FuncOnBeforeSwitch {
	ptr := xatomic.LoadPointer(&s.OnBeforeSwitch)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetOnBeforeSwitch(fn FuncOnBeforeSwitch) {
	xatomic.StorePointer(&s.OnBeforeSwitch, ptr(fn))
}

func (s *Switch) GetOnInterruptedSwitch() FuncOnInterruptedSwitch {
	ptr := xatomic.LoadPointer(&s.OnInterruptedSwitch)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetOnInterruptedSwitch(fn FuncOnInterruptedSwitch) {
	xatomic.StorePointer(&s.OnInterruptedSwitch, ptr(fn))
}

func (s *Switch) GetOnAfterSwitch() FuncOnAfterSwitch {
	ptr := xatomic.LoadPointer(&s.OnAfterSwitch)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetOnAfterSwitch(fn FuncOnAfterSwitch) {
	xatomic.StorePointer(&s.OnAfterSwitch, ptr(fn))
}

func (s *Switch) GetValue(
	ctx context.Context,
) int32 {
	return s.CurrentValue.Load()
}

func (s *Switch) SetValue(
	ctx context.Context,
	idx int32,
) (_err error) {
	logger.Tracef(ctx, "Switch.SetValue(ctx, %d)", idx)
	defer func() { logger.Tracef(ctx, "/Switch.SetValue(ctx, %d): %v", idx, _err) }()

	if onSwitchRequest := s.GetOnSwitchRequest(); onSwitchRequest != nil {
		err := xsync.DoA3R1(ctx, &s.CommitMutex, onSwitchRequest, ctx, packetorframe.InputUnion{}, idx)
		if err != nil {
			return fmt.Errorf("onSwitchRequest failed: %w", err)
		}
	}

	if s.GetKeepUnless() == nil {
		return s.setValueNow(ctx, idx)
	}

	return s.setNextValueNow(ctx, idx)
}

func (s *Switch) setValueNow(
	ctx context.Context,
	idx int32,
) (_err error) {
	s.CommitMutex.Do(ctx, func() {
		if onBefore := s.GetOnBeforeSwitch(); onBefore != nil {
			onBefore(ctx, packetorframe.InputUnion{}, s.CurrentValue.Load(), idx)
		}

		old := s.CurrentValue.Swap(int32(idx))
		logger.Debugf(ctx, "switch to the current value: %d -> %d", old, idx)
		if old == idx {
			if onInterruption := s.GetOnInterruptedSwitch(); onInterruption != nil {
				onInterruption(ctx, packetorframe.InputUnion{}, old, idx)
			}
			return
		}

		if s.Flags.HasAny(types.SwitchFlagFirstPacketAfterSwitchPassBothOutputs) {
			s.PreviousValue.Store(old)
			s.FirstPacketOrFrameAfterSwitch = nil
		}

		s.rotateChangeChan()

		onAfter := s.GetOnAfterSwitch()
		if onAfter != nil {
			onAfter(ctx, packetorframe.InputUnion{}, old, idx)
		}
	})
	return nil
}

func (s *Switch) setNextValueNow(
	ctx context.Context,
	idx int32,
) (_err error) {
	s.CommitMutex.Do(ctx, func() {
		old := s.NextValue.Swap(int32(idx))
		logger.Debugf(ctx, "setting the next value: %d -> %d (cur: %d)", old, idx, s.CurrentValue.Load())
		if old == idx {
			return
		}
		if s.CurrentValue.Load() == idx {
			logger.Debugf(ctx, "the next value is equal to the current value")
			if old == math.MinInt32 {
				return
			}
			s.NextValue.Store(math.MinInt32)
		}
		s.rotateChangeChan()
	})
	return nil
}

type SwitchOutput struct {
	*Switch
	OutputID int32
}

func (s *Switch) Output(OutputID int32) *SwitchOutput {
	return &SwitchOutput{
		Switch:   s,
		OutputID: OutputID,
	}
}

func (s *Switch) GetChangeChan() <-chan struct{} {
	return *xatomic.LoadPointer(&s.ChangeSignal)
}

func (s *Switch) rotateChangeChan() {
	close(*xatomic.SwapPointer(&s.ChangeSignal, ptr(make(chan struct{}))))
}

func (s *SwitchOutput) GetState(
	ctx context.Context,
	pkt packetorframe.InputUnion,
) (_ret0 types.State, _ret1 <-chan struct{}) {
	logger.Tracef(ctx, "GetState[%p:%v](ctx, %v)", s.Switch, s.OutputID, pkt)
	defer func() {
		logger.Tracef(ctx, "/GetState[%p:%v](ctx, %v): %v, %p", s.Switch, s.OutputID, pkt, _ret0, _ret1)
	}()

	for {
		currentValue := s.CurrentValue.Load()
		nextValue := s.NextValue.Load()
		previousValue := s.PreviousValue.Load()

		constructState := func() (types.State, <-chan struct{}) {
			if currentValue == s.OutputID {
				return types.StatePass, s.GetChangeChan()
			}
			if s.Flags.HasAny(types.SwitchFlagFirstPacketAfterSwitchPassBothOutputs) {
				// allow the previous output (the one that used to be current) to pass the first packet after a switch
				if previousValue == s.OutputID && pkt.Get() == s.FirstPacketOrFrameAfterSwitch {
					s.CommitMutex.Do(ctx, func() {
						s.FirstPacketOrFrameAfterSwitch = nil
						s.PreviousValue.Store(math.MinInt32)
					})
					return types.StatePass, s.GetChangeChan()
				}
			}
			if currentValue == nextValue {
				if s.Flags.HasAny(types.SwitchFlagNextOutputStateBlock) {
					return types.StateBlock, s.GetChangeChan()
				}
			}
			if s.Flags.HasAny(types.SwitchFlagInactiveBlock) {
				return types.StateBlock, s.GetChangeChan()
			}
			return types.StateDrop, s.GetChangeChan()
		}

		commitToNextValue := func() (_ret bool) {
			s.CommitMutex.Do(ctx, func() {
				logger.Debugf(ctx, "found a good entrance and switched to the next value: %d -> %d", currentValue, nextValue)
				if onBefore := s.GetOnBeforeSwitch(); onBefore != nil {
					onBefore(ctx, pkt, currentValue, nextValue)
				}
				if onInterruption := s.GetOnInterruptedSwitch(); onInterruption != nil {
					defer func() {
						if !_ret {
							onInterruption(ctx, pkt, currentValue, nextValue)
						}
					}()
				}
				if !s.CurrentValue.CompareAndSwap(int32(currentValue), int32(nextValue)) {
					logger.Debugf(ctx, "the current value changed concurrently; restarting the calculation")
					return
				}
				if !s.NextValue.CompareAndSwap(nextValue, math.MinInt32) {
					logger.Errorf(ctx, "the next value changed concurrently; restarting the calculation (reaching this line was supposed to be impossible, though)")
					return
				}
				if s.Flags.HasAny(types.SwitchFlagFirstPacketAfterSwitchPassBothOutputs) {
					s.PreviousValue.Store(currentValue)
					s.FirstPacketOrFrameAfterSwitch = pkt.Get()
				}
				s.rotateChangeChan()
				currentValue, previousValue = nextValue, currentValue
				onAfter := s.GetOnAfterSwitch()
				if onAfter != nil {
					onAfter(ctx, pkt, previousValue, currentValue)
				}
				_ret = true
			})
			return
		}

		if s.Flags.HasAny(types.SwitchFlagFirstPacketAfterSwitchPassBothOutputs) {
			if previousValue != math.MinInt32 && s.FirstPacketOrFrameAfterSwitch == nil {
				s.FirstPacketOrFrameAfterSwitch = pkt.Get()
			}
		}

		if s.Flags.HasAny(types.SwitchFlagForbidTakeoverInKeepUnless) {
			if currentValue != s.OutputID {
				return constructState()
			}
		}

		// == switch if needed ==

		if nextValue == math.MinInt32 { // no next value
			return constructState()
		}

		if nextValue == currentValue {
			return constructState()
		}

		keepUnless := s.GetKeepUnless()
		if keepUnless != nil && !keepUnless.Match(ctx, pkt) {
			return constructState()
		}

		if !commitToNextValue() {
			continue
		}
		return constructState()
	}
}

func (s *SwitchOutput) String() string {
	currentValue := s.CurrentValue.Load()
	return fmt.Sprintf(
		"SwitchOutput(%t: req:%d; cur:%d)",
		currentValue == s.OutputID,
		currentValue,
		s.OutputID,
	)
}
