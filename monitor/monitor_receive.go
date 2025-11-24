//go:build with_libav
// +build with_libav

package monitor

import (
	"context"
	"fmt"

	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

func (m *Monitor) asPacketFilterCondition() packetfiltercondition.Condition {
	return (*monitorAsPacketFilterCondition)(m)
}

func (m *Monitor) asFrameFilterCondition() framefiltercondition.Condition {
	return (*monitorAsFrameFilterCondition)(m)
}

type monitorAsPacketFilterCondition Monitor

func (m *monitorAsPacketFilterCondition) asMonitor() *Monitor {
	return (*Monitor)(m)
}

func (m *monitorAsPacketFilterCondition) String() string {
	return fmt.Sprintf("Monitor(%s)", m.Object)
}

func (m *monitorAsPacketFilterCondition) Match(
	ctx context.Context,
	in packetfiltercondition.Input,
) bool {
	m.asMonitor().ObserveInputPacket(ctx, in.Input)
	return true
}

type monitorAsFrameFilterCondition Monitor

func (m *monitorAsFrameFilterCondition) asMonitor() *Monitor {
	return (*Monitor)(m)
}

func (m *monitorAsFrameFilterCondition) String() string {
	return fmt.Sprintf("Monitor(%s)", m.Object)
}

func (m *monitorAsFrameFilterCondition) Match(
	ctx context.Context,
	in framefiltercondition.Input,
) bool {
	m.asMonitor().ObserveInputFrame(ctx, in.Input)
	return true
}
