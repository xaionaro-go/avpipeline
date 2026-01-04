//go:build with_libav
// +build with_libav

// monitor_receive.go implements the input filter condition for monitoring received packets and frames.

package monitor

import (
	"context"
	"fmt"

	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

func (m *Monitor) asInputFilterCondition() packetorframefiltercondition.Condition {
	return (*monitorAsInputFilterCondition)(m)
}

type monitorAsInputFilterCondition Monitor

func (m *monitorAsInputFilterCondition) asMonitor() *Monitor {
	return (*Monitor)(m)
}

func (m *monitorAsInputFilterCondition) String() string {
	return fmt.Sprintf("Monitor(%s)", m.Object)
}

func (m *monitorAsInputFilterCondition) Match(
	ctx context.Context,
	in packetorframefiltercondition.Input,
) bool {
	if in.Input.Packet != nil {
		m.asMonitor().ObserveInputPacket(ctx, *in.Input.Packet)
	}
	if in.Input.Frame != nil {
		m.asMonitor().ObserveInputFrame(ctx, *in.Input.Frame)
	}
	return true
}
