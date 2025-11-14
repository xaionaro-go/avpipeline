package monitor

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
)

type monitorAsOutputMonitor Monitor

var _ kernel.OutputPacketMonitor = (*monitorAsOutputMonitor)(nil)

func (m *Monitor) asOutputMonitor() kernel.OutputPacketMonitor {
	return (*monitorAsOutputMonitor)(m)
}

func (m *monitorAsOutputMonitor) asMonitor() *Monitor {
	return (*Monitor)(m)
}

func (m *monitorAsOutputMonitor) ObserveOutputPacket(
	ctx context.Context,
	stream *astiav.Stream,
	output *astiav.Packet,
) {
	err := m.asMonitor().observePacket(ctx, output, &packet.StreamInfo{
		Stream: stream,
	})
	if err != nil {
		logger.Errorf(ctx, "observe output packet error: %v", err)
	}
}
