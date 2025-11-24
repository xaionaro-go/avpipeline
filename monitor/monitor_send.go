//go:build with_libav
// +build with_libav

package monitor

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type MonitorNode = node.Node[*processor.FromKernel[*monitorAsKernel]]

func (m *Monitor) asKernel() *monitorAsKernel {
	return (*monitorAsKernel)(m)
}

func (m *Monitor) newNode(
	ctx context.Context,
) *MonitorNode {
	return node.NewFromKernel(ctx, m.asKernel())
}

type monitorAsKernel Monitor

var _ globaltypes.ErrorHandler = (*monitorAsKernel)(nil)

func (m *monitorAsKernel) asMonitor() *Monitor {
	return (*Monitor)(m)
}

func (m *monitorAsKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(m)
}

func (m *monitorAsKernel) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	m.asMonitor().ObserveInputPacket(ctx, input)
	return nil
}

func (m *monitorAsKernel) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	m.asMonitor().ObserveInputFrame(ctx, input)
	return nil
}

func (m *monitorAsKernel) String() string {
	return fmt.Sprintf("Monitor(%s)", m.Object)
}

func (m *monitorAsKernel) Close(ctx context.Context) (_err error) {
	m.KernelClosureSignaler.Close(ctx)
	return nil
}

func (m *monitorAsKernel) CloseChan() <-chan struct{} {
	return m.KernelClosureSignaler.CloseChan()
}

func (m *monitorAsKernel) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	return nil
}

func (m *monitorAsKernel) HandleError(
	ctx context.Context,
	err error,
) error {
	switch {
	case errors.Is(err, astiav.ErrEof):
		logger.Debugf(ctx, "monitor kernel received EOF")
		return nil
	case errors.Is(err, context.Canceled):
		logger.Debugf(ctx, "monitor kernel received context.Canceled")
		return nil
	default:
		logger.Errorf(ctx, "monitor kernel received error: %v", err)
	}
	return nil
}
