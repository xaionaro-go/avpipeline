// switch.go implements a condition that can switch between values based on packet matching.

package condition

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
)

type FuncOnAfterSwitch func(ctx context.Context, pkt packet.Input, from int32, to int32)

type Switch struct {
	CurrentValue atomic.Int32
	NextValue    atomic.Int32

	KeepUnlessPacketCond *Condition
	OnAfterSwitch        *FuncOnAfterSwitch
}

func NewSwitch() *Switch {
	return &Switch{}
}

var _ Condition = (*Switch)(nil).PacketCondition(0)

func (s *Switch) GetKeepUnless() Condition {
	ptr := xatomic.LoadPointer(&s.KeepUnlessPacketCond)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Switch) SetKeepUnless(cond Condition) {
	xatomic.StorePointer(&s.KeepUnlessPacketCond, ptr(cond))
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
		logger.Debugf(ctx, "switched to the next value: %d -> %d", s.CurrentValue.Load(), idx)
		s.CurrentValue.Store(int32(idx))
	}
	return nil
}

type SwitchPacketCondition struct {
	*Switch
	RequiredValue int32
}

func (s *Switch) PacketCondition(requiredValue int32) *SwitchPacketCondition {
	return &SwitchPacketCondition{
		Switch:        s,
		RequiredValue: requiredValue,
	}
}

func (s *SwitchPacketCondition) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	currentValue := s.CurrentValue.Load()
	nextValue := s.NextValue.Load()
	if nextValue == math.MinInt32 {
		return currentValue == s.RequiredValue
	}

	commitToNextValue := func() {
		logger.Debugf(ctx, "found a good entrance and switched to the next value: %d -> %d", currentValue, nextValue)
		s.CurrentValue.Store(int32(nextValue))
		if !s.NextValue.CompareAndSwap(nextValue, math.MinInt32) {
			return
		}
		onAfter := s.GetOnAfterSwitch()
		if onAfter != nil {
			onAfter(ctx, pkt, currentValue, nextValue)
		}
	}

	if nextValue == currentValue {
		return currentValue == s.RequiredValue
	}

	keepUnless := s.GetKeepUnless()
	if keepUnless != nil && !keepUnless.Match(ctx, pkt) {
		return currentValue == s.RequiredValue
	}

	commitToNextValue()
	return currentValue == s.RequiredValue
}

func (s *SwitchPacketCondition) String() string {
	currentValue := s.CurrentValue.Load()
	return fmt.Sprintf("SwitchCondition(%t: req:%d; cur:%d)", currentValue == s.RequiredValue, currentValue, s.RequiredValue)
}
