//go:build with_libav
// +build with_libav

// monitor.go implements the main monitoring logic for the media pipeline.

// Package monitor provides monitoring and event reporting for the media pipeline.
package monitor

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/internal"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	goconvavp "github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipeline"
	goconvlibav "github.com/xaionaro-go/avpipeline/protobuf/goconv/libav"
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
	DecodedOutput         chan packetorframe.OutputUnion
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
	if node == nil {
		return nil, fmt.Errorf("node is nil")
	}
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
		m.DecodedOutput = make(chan packetorframe.OutputUnion, 100)
		m.Decoder = kernel.NewDecoder(ctx, codec.NewNaiveDecoderFactory(ctx,
			&codec.NaiveDecoderFactoryParams{
				ErrorRecognitionFlags: astiav.ErrorRecognitionFlags(astiav.ErrorRecognitionFlagIgnoreErr),
			},
		))
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
		assert(ctx, m.Object.GetInputFilter(ctx) == nil, "input filter is already set for node %v", m.Object.GetObjectID())
		m.Object.SetInputFilter(ctx, m.asInputFilterCondition())
		return nil
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_SEND:
		assert(ctx, m.Node == nil, "internal error: push to monitor is already set for node %v", m.Object.GetObjectID())
		m.Node = m.newNode(ctx)
		m.Object.AddPushTo(ctx, m.Node)
		return nil
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_KERNEL_OUTPUT_SEND:
		k, err := getOutputMonitorer(ctx, m.Object)
		if err != nil {
			return fmt.Errorf("node %v does not support output monitor: %w", m.Object.GetObjectID(), err)
		}
		assert(ctx, k.GetOutputMonitor(ctx) == nil, "output monitor is already set for node %v: %#+v", m.Object.GetObjectID(), k.GetOutputMonitor(ctx))
		k.SetOutputMonitor(ctx, m.asOutputMonitor())
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
		assert(ctx, m.Object.GetInputFilter(ctx) != nil, "input filter is already unset for node %v: %#+v", m.Object.GetObjectID(), m.Object.GetInputFilter(ctx))
		m.Object.SetInputFilter(ctx, nil)
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_SEND:
		n := m.Node
		assert(ctx, n != nil, "internal error: push to monitor is already unset for node %v", m.Object.GetObjectID())
		assert(ctx, m.Object.RemovePushTo(ctx, n) == nil, "push to monitor is already unset for node %v", m.Object.GetObjectID())
		m.Node = nil
	case avpipelinegrpc.MonitorEventType_EVENT_TYPE_KERNEL_OUTPUT_SEND:
		k, err := getOutputMonitorer(ctx, m.Object)
		if err != nil {
			return fmt.Errorf("node %v does not support output monitor: %w", m.Object.GetObjectID(), err)
		}
		assert(ctx, k.GetOutputMonitor(ctx) != nil, "output monitor is already unset for node %v", m.Object.GetObjectID())
		k.SetOutputMonitor(ctx, nil)
		return nil
	default:
		return fmt.Errorf("unsupported monitor event type: %s", m.Type)
	}
	return nil
}

func getOutputMonitorer(
	ctx context.Context,
	n node.Abstract,
) (kernel.OutputMonitorer, error) {
	m, ok := n.(kernel.OutputMonitorer)
	if ok {
		return m, nil
	}

	proc, ok := n.GetProcessor().(kernel.GetKerneler)
	if !ok {
		return nil, fmt.Errorf("node %v does not support getting kernel", n.GetObjectID())
	}

	m, ok = proc.GetKernel().(kernel.OutputMonitorer)
	if ok {
		return m, nil
	}

	getKernelser, ok := proc.GetKernel().(kernel.GetKernelser)
	if ok {
		for _, k := range slices.Backward(getKernelser.GetKernels()) {
			m, ok = k.(kernel.OutputMonitorer)
			if ok {
				return m, nil
			}
		}
	}

	return nil, fmt.Errorf("node %v does not support output monitor", n.GetObjectID())
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
		errCh <- m.Decoder.SendInput(ctx, packetorframe.InputUnion{Packet: &in}, m.DecodedOutput)
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
		case out := <-m.DecodedOutput:
			if out.Frame != nil {
				frames = append(frames, out.Frame.Frame)
			}
		}
	}
}

func (m *Monitor) observePacket(
	ctx context.Context,
	pkt *astiav.Packet,
	streamInfo *packet.StreamInfo,
) (_err error) {
	if streamInfo == nil {
		return fmt.Errorf("streamInfo is nil")
	}
	var sourceKernelID globaltypes.ObjectID
	if _, ok := streamInfo.Source.(globaltypes.GetObjectIDer); ok {
		sourceKernelID = streamInfo.Source.(globaltypes.GetObjectIDer).GetObjectID()
	}
	psd, err := goconvavp.PipelineSideDataFromGo(streamInfo.PipelineSideData)
	if err != nil {
		logger.Errorf(ctx, "monitor: failed to convert pipeline side data: %v", err)
	}
	event := &avpipelinegrpc.MonitorEvent{
		TimestampNs:      uint64(time.Now().UnixNano()),
		SourceKernelId:   uint64(sourceKernelID),
		Stream:           goconvlibav.StreamFromGo(streamInfo.Stream).Protobuf(),
		Packet:           goconvlibav.PacketFromGo(pkt, m.IncludePacketPayload).Protobuf(),
		PipelineSideData: psd.Protobuf(),
	}
	if m.DoDecode {
		frames, err := m.decodeFrames(ctx, pkt, streamInfo)
		if err != nil {
			return fmt.Errorf("unable to decode packet for monitoring: %w", err)
		}
		for _, fr := range frames {
			event.Frames = append(event.Frames, goconvlibav.FrameFromGo(fr, m.IncludeFramePayload).Protobuf())
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
	if streamInfo == nil {
		return fmt.Errorf("streamInfo is nil")
	}
	var sourceKernelID globaltypes.ObjectID
	if _, ok := streamInfo.Source.(globaltypes.GetObjectIDer); ok {
		sourceKernelID = streamInfo.Source.(globaltypes.GetObjectIDer).GetObjectID()
	}
	psd, err := goconvavp.PipelineSideDataFromGo(streamInfo.PipelineSideData)
	if err != nil {
		logger.Errorf(ctx, "monitor: failed to convert pipeline side data: %v", err)
	}
	event := &avpipelinegrpc.MonitorEvent{
		TimestampNs:    uint64(time.Now().UnixNano()),
		SourceKernelId: uint64(sourceKernelID),
		Stream: &libav_proto.Stream{
			Index:           int32(streamInfo.StreamIndex),
			CodecParameters: goconvlibav.CodecParametersFromGo(streamInfo.CodecParameters).Protobuf(),
			TimeBase:        goconvlibav.RationalFromGo(ptr(streamInfo.TimeBase)).Protobuf(),
			Duration:        streamInfo.Duration,
		},
		Frames:           []*libav_proto.Frame{goconvlibav.FrameFromGo(fr, m.IncludeFramePayload).Protobuf()},
		PipelineSideData: psd.Protobuf(),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.Events <- event:
	default:
		// Drop the event if the buffer is full to avoid stalling the pipeline
	}
	return nil
}
