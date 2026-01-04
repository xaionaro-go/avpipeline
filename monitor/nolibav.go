//go:build !with_libav
// +build !with_libav

// nolibav.go provides a stub implementation of the monitor when libav is not available.

package monitor

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

type Monitor struct {
	Events chan *avpipelinegrpc.MonitorEvent
}

func New(
	ctx context.Context,
	node node.Abstract,
	t avpipelinegrpc.MonitorEventType,
	includePacketPayload bool,
	includeFramePayload bool,
	doDecode bool,
) (*Monitor, error) {
	return nil, fmt.Errorf("monitoring is not supported without libav support")
}

func (m *Monitor) Close(ctx context.Context) error {
	return fmt.Errorf("monitoring is not supported without libav support")
}

func (m *Monitor) ObserveInputPacket(
	ctx context.Context,
	pkt packet.Input,
) {
}

func (m *Monitor) ObserveInputFrame(
	ctx context.Context,
	frame frame.Input,
) {
}
