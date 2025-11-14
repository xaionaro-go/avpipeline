package monitor

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	"github.com/xaionaro-go/avpipeline/protobuf/goconv"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

type Monitor struct {
	Object                node.Abstract
	Events                chan *avpipelinegrpc.MonitorEvent
	Type                  avpipelinegrpc.MonitorEventType
	IncludePacketPayload  bool
	IncludeFramePayload   bool
	DoDecode              bool
	Decoder               *kernel.Decoder[*codec.NaiveDecoderFactory]
	DecodedPackets        chan packet.Output
	DecodedFrames         chan frame.Output
	KernelClosureSignaler *closuresignaler.ClosureSignaler
	Node                  *MonitorNode
}

func New(
	ctx context.Context,
	node node.Abstract,
	t avpipelinegrpc.MonitorEventType,
	includePacketPayload bool,
	includeFramePayload bool,
	doDecode bool,
) (*Monitor, error) {
	m := &Monitor{
		Object: node,
		Events: make(chan *avpipelinegrpc.MonitorEvent, 100),
		Type:   t,

		IncludePacketPayload:  includePacketPayload,
		IncludeFramePayload:   includeFramePayload,
		DoDecode:              doDecode,
		KernelClosureSignaler: closuresignaler.New(),
	}
	if doDecode {
		m.DecodedPackets = make(chan packet.Output, 100)
		m.DecodedFrames = make(chan frame.Output, 100)
		m.Decoder = kernel.NewDecoder(ctx, codec.NewNaiveDecoderFactory(ctx, nil))
		internal.SetFinalizerClose(ctx, m.Decoder)
	}
	err := m.inject(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to inject monitor %s into node %v: %w", t, node.GetObjectID(), err)
	}
	return m, nil
}

func (m *Monitor) Close(ctx context.Context) error {
	// TODO: close the m.Decoder here, instead of waiting for the GC to do it
	return m.uninject(ctx)
}

func (m *Monitor) inject(
	ctx context.Context,
) error {
	switch m.Type {
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_RECEIVE:
		assert(ctx, m.Object.GetInputPacketFilter() == nil, "input packet filter is already set for node %v", m.Object.GetObjectID())
		assert(ctx, m.Object.GetInputFrameFilter() == nil, "input frame filter is already set for node %v", m.Object.GetObjectID())
		m.Object.SetInputPacketFilter(m.asPacketFilterCondition())
		m.Object.SetInputFrameFilter(m.asFrameFilterCondition())
		return nil
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_SEND:
		assert(ctx, m.Node == nil, "internal error: push to monitor is already set for node %v", m.Object.GetObjectID())
		m.Node = m.newNode(ctx)
		m.Object.AddPushPacketsTo(ctx, m.Node)
		m.Object.AddPushFramesTo(ctx, m.Node)
		return nil
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_KERNEL_OUTPUT_SEND:
		s, ok := m.Object.(kernel.OutputMonitorer)
		if !ok {
			return fmt.Errorf("node %v does not support setting output monitor", m.Object.GetObjectID())
		}
		assert(ctx, s.GetOutputMonitor(ctx) != nil, "output monitor is already set for node %v", m.Object.GetObjectID())
		s.SetOutputMonitor(ctx, m.asOutputMonitor())
		return nil
	default:
		return fmt.Errorf("unsupported monitor event type: %s", m.Type)
	}
}

func (m *Monitor) uninject(
	ctx context.Context,
) error {
	switch m.Type {
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_RECEIVE:
		assert(ctx, m.Object.GetInputPacketFilter() != nil, "input packet filter is already unset for node %v", m.Object.GetObjectID())
		assert(ctx, m.Object.GetInputFrameFilter() != nil, "input frame filter is already unset for node %v", m.Object.GetObjectID())
		m.Object.SetInputPacketFilter(nil)
		m.Object.SetInputFrameFilter(nil)
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_SEND:
		n := m.Node
		assert(ctx, n != nil, "internal error: push to monitor is already unset for node %v", m.Object.GetObjectID())
		assert(ctx, m.Object.RemovePushPacketsTo(ctx, n) == nil, "push packets to monitor is already unset for node %v", m.Object.GetObjectID())
		assert(ctx, m.Object.RemovePushFramesTo(ctx, n) == nil, "push frames to monitor is already unset for node %v", m.Object.GetObjectID())
		m.Node = nil
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_KERNEL_OUTPUT_SEND:
		s, ok := m.Object.(kernel.OutputMonitorer)
		if !ok {
			return fmt.Errorf("node %v does not support setting output monitor", m.Object.GetObjectID())
		}
		assert(ctx, s.GetOutputMonitor(ctx) == nil, "output monitor is already unset for node %v", m.Object.GetObjectID())
		s.SetOutputMonitor(ctx, nil)
		return nil
	default:
		return fmt.Errorf("unsupported monitor event type: %s", m.Type)
	}
	return nil
}

func (m *Monitor) ObserveInputPacket(
	ctx context.Context,
	pkt packet.Input,
) {
	err := m.observePacket(ctx, pkt.Packet, pkt.StreamInfo)
	if err != nil {
		logger.Errorf(ctx, "monitor observe input packet failed: %v", err)
		return
	}
}

func (m *Monitor) decodeFrames(
	ctx context.Context,
	pkt *astiav.Packet,
	streamInfo *packet.StreamInfo,
) ([]*astiav.Frame, error) {
	errCh := make(chan error, 1)
	in := packet.BuildInput(pkt, streamInfo)
	observability.Go(ctx, func(ctx context.Context) {
		errCh <- m.Decoder.SendInputPacket(ctx, in, m.DecodedPackets, m.DecodedFrames)
	})
	var frames []*astiav.Frame
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			if err != nil {
				return frames, fmt.Errorf("unable to send packet to decoder: %w", err)
			}
			return frames, nil
		case outFrame := <-m.DecodedFrames:
			frames = append(frames, outFrame.Frame)
		}
	}
}

func (m *Monitor) observePacket(
	ctx context.Context,
	pkt *astiav.Packet,
	streamInfo *packet.StreamInfo,
) (_err error) {
	var sourceKernelID globaltypes.ObjectID
	if _, ok := streamInfo.Source.(globaltypes.GetObjectIDer); ok {
		sourceKernelID = streamInfo.Source.(globaltypes.GetObjectIDer).GetObjectID()
	}
	event := &avpipelinegrpc.MonitorEvent{
		SourceKernelId:   uint64(sourceKernelID),
		Stream:           goconv.StreamFromGo(streamInfo.Stream).Protobuf(),
		Packet:           goconv.PacketFromGo(pkt, m.IncludePacketPayload).Protobuf(),
		PipelineSideData: goconv.PipelineSideDataFromGo(streamInfo.PipelineSideData).Protobuf(),
	}
	if m.DoDecode {
		frames, err := m.decodeFrames(ctx, pkt, streamInfo)
		if err != nil {
			return fmt.Errorf("unable to decode packet for monitoring: %w", err)
		}
		for _, fr := range frames {
			event.Frames = append(event.Frames, goconv.FrameFromGo(fr, m.IncludeFramePayload).Protobuf())
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.Events <- event:
	}
	return nil
}

func (m *Monitor) ObserveInputFrame(
	ctx context.Context,
	frame frame.Input,
) {
	m.observeFrame(ctx, frame.Frame, frame.StreamInfo)
}

func (m *Monitor) observeFrame(
	ctx context.Context,
	fr *astiav.Frame,
	streamInfo *frame.StreamInfo,
) (_err error) {
	var sourceKernelID globaltypes.ObjectID
	if _, ok := streamInfo.Source.(globaltypes.GetObjectIDer); ok {
		sourceKernelID = streamInfo.Source.(globaltypes.GetObjectIDer).GetObjectID()
	}
	event := &avpipelinegrpc.MonitorEvent{
		SourceKernelId: uint64(sourceKernelID),
		Stream: &libav_proto.Stream{
			Index:           int32(streamInfo.StreamIndex),
			CodecParameters: goconv.CodecParametersFromGo(streamInfo.CodecParameters).Protobuf(),
			TimeBase:        goconv.RationalFromGo(ptr(streamInfo.TimeBase)).Protobuf(),
			Duration:        streamInfo.Duration,
		},
		Frames:           []*libav_proto.Frame{goconv.FrameFromGo(fr, m.IncludeFramePayload).Protobuf()},
		PipelineSideData: goconv.PipelineSideDataFromGo(streamInfo.PipelineSideData).Protobuf(),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.Events <- event:
	}
	return nil
}
