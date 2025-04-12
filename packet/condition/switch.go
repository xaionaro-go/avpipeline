package condition

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Switch struct {
	CurrentValue atomic.Int32
	NextValue    atomic.Int32

	KeepUnlessPacketCond *Condition
}

func NewSwitch() *Switch {
	return &Switch{}
}

var _ Condition = (*Switch)(nil).Condition(0)

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

func (s *Switch) GetValue(
	ctx context.Context,
) int32 {
	return s.CurrentValue.Load()
}

func (s *Switch) SetValue(
	ctx context.Context,
	idx int32,
) error {
	if s.GetKeepUnless() == nil {
		logger.Debugf(ctx, "switched to the next value: %d -> %d", s.CurrentValue.Load(), idx)
		s.CurrentValue.Store(int32(idx))
		return nil
	}
	logger.Debugf(ctx, "setting the next value: %d -> %d", s.CurrentValue.Load(), idx)
	s.NextValue.Store(int32(idx))
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

func (s *SwitchCondition) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	currentValue := s.CurrentValue.Load()
	nextValue := s.NextValue.Load()
	if nextValue == math.MinInt32 {
		return currentValue == s.RequiredValue
	}

	commitToNextKernel := func() {
		logger.Debugf(ctx, "found a good entrance and switched to the next value: %d -> %d", currentValue, nextValue)
		s.CurrentValue.Store(int32(nextValue))
		s.NextValue.CompareAndSwap(nextValue, math.MinInt32)
	}

	if nextValue == currentValue {
		commitToNextKernel()
		return currentValue == s.RequiredValue
	}

	keepUnless := s.GetKeepUnless()
	if keepUnless != nil && !keepUnless.Match(ctx, pkt) {
		return currentValue == s.RequiredValue
	}

	return currentValue == s.RequiredValue
}

func (s *SwitchCondition) String() string {
	currentValue := s.CurrentValue.Load()
	return fmt.Sprintf("SwitchCondition(%t: req:%d; cur:%d)", currentValue == s.RequiredValue, currentValue, s.RequiredValue)
}
