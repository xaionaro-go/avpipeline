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

type FuncOnAfterSwitch func(ctx context.Context, pkt packetorframe.InputUnion, from int32, to int32)

type Switch struct {
	CurrentValue atomic.Int32
	NextValue    atomic.Int32
	IsReleased   atomic.Bool

	ChangeSignal         *chan struct{}
	KeepUnlessPacketCond *packetorframecondition.Condition
	AutoReleaseCond      *packetorframecondition.Condition
	OnAfterSwitch        *FuncOnAfterSwitch

	CommitMutex xsync.Mutex
}

func NewSwitch() *Switch {
	return &Switch{
		ChangeSignal: ptr(make(chan struct{})),
	}
}

var _ StateGetter = (*Switch)(nil).Condition(0)

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

func (s *Switch) GetAutoRelease() packetorframecondition.Condition {
	ptr := xatomic.LoadPointer(&s.AutoReleaseCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetAutoRelease(cond packetorframecondition.Condition) {
	xatomic.StorePointer(&s.AutoReleaseCond, ptr(cond))
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
) error {
	logger.Debugf(ctx, "setting the next value: %d -> %d", s.CurrentValue.Load(), idx)
	s.NextValue.Store(int32(idx))
	if s.GetKeepUnless() == nil {
		logger.Debugf(ctx, "BarrierConditionSwitched to the next value: %d -> %d", s.CurrentValue.Load(), idx)
		s.CurrentValue.Store(int32(idx))
	}
	return nil
}

type SwitchCondition struct {
	*Switch
	RequiredValue int32
}

func (s *Switch) Condition(requiredValue int32) *SwitchCondition {
	return &SwitchCondition{
		Switch:        s,
		RequiredValue: requiredValue,
	}
}

func (s *Switch) GetChangeChan() <-chan struct{} {
	return *xatomic.LoadPointer(&s.ChangeSignal)
}

func (s *Switch) rotateChangeChan() {
	close(*xatomic.SwapPointer(&s.ChangeSignal, ptr(make(chan struct{}))))
}

func (s *Switch) Release() {
	if !s.IsReleased.CompareAndSwap(false, true) {
		return
	}
	s.rotateChangeChan()
}

func (s *SwitchCondition) GetState(
	ctx context.Context,
	pkt packetorframe.InputUnion,
) (types.State, <-chan struct{}) {
	currentValue := s.CurrentValue.Load()
	nextValue := s.NextValue.Load()

	constructState := func() (types.State, <-chan struct{}) {
		isReleased := s.IsReleased.Load()
		if !isReleased && s.GetAutoRelease() != nil && s.GetAutoRelease().Match(ctx, pkt) {
			s.Release()
		}
		if currentValue != s.RequiredValue {
			if !isReleased && currentValue == nextValue {
				return types.StateBlock, s.GetChangeChan()
			}
			return types.StateDrop, s.GetChangeChan()
		}
		if isReleased {
			return types.StatePass, s.GetChangeChan()
		}
		return types.StateBlock, s.GetChangeChan()
	}

	if nextValue == math.MinInt32 { // no next value
		return constructState()
	}

	commitToNextValue := func() {
		s.CommitMutex.Do(ctx, func() {
			logger.Debugf(ctx, "found a good entrance and switched to the next value: %d -> %d", currentValue, nextValue)
			s.CurrentValue.Store(int32(nextValue))
			if !s.NextValue.CompareAndSwap(nextValue, math.MinInt32) {
				return
			}
			if s.GetAutoRelease() != nil && s.GetAutoRelease().Match(ctx, pkt) {
				logger.Debugf(ctx, "AutoRelease condition matched, releasing the switch")
				s.IsReleased.Store(true)
			} else {
				s.IsReleased.Store(false)
			}
			s.rotateChangeChan()
			onAfter := s.GetOnAfterSwitch()
			if onAfter != nil {
				onAfter(ctx, pkt, currentValue, nextValue)
			}
		})
	}

	if nextValue == currentValue {
		return constructState()
	}

	keepUnless := s.GetKeepUnless()
	if keepUnless != nil && !keepUnless.Match(ctx, pkt) {
		return constructState()
	}

	commitToNextValue()
	return constructState()
}

func (s *SwitchCondition) String() string {
	currentValue := s.CurrentValue.Load()
	return fmt.Sprintf(
		"SwitchCondition(%t: req:%d; cur:%d)",
		currentValue == s.RequiredValue,
		currentValue,
		s.RequiredValue,
	)
}
