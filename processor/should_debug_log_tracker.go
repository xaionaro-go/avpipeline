// should_debug_log_tracker.go tracks per-stream debug log deduplication for packets and frames.

package processor

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/xsync"
)

// shouldDebugLogTracker tracks which (direction, type, streamIndex) combinations
// have already been logged, so we log only the first packet/frame per stream.
type shouldDebugLogTracker struct {
	inputPackets  xsync.Map[int, struct{}]
	inputFrames   xsync.Map[int, struct{}]
	outputPackets xsync.Map[int, struct{}]
	outputFrames  xsync.Map[int, struct{}]
}

func (t *shouldDebugLogTracker) logFirstInput(
	ctx context.Context,
	name fmt.Stringer,
	input packetorframe.InputUnion,
) {
	switch {
	case input.Packet != nil && input.Packet.Packet != nil:
		streamIdx := input.GetStreamIndex()
		if _, loaded := t.inputPackets.LoadOrStore(streamIdx, struct{}{}); !loaded {
			logger.Debugf(ctx, "[%s] first input packet (stream %d): mediaType=%s pts=%d dts=%d duration=%d size=%d timeBase=%v isKey=%v",
				name, streamIdx, input.GetMediaType(), input.GetPTS(), input.GetDTS(), input.GetDuration(), input.GetSize(), input.GetTimeBase(), input.IsKey())
		}
	case input.Frame != nil && input.Frame.Frame != nil:
		streamIdx := input.GetStreamIndex()
		if _, loaded := t.inputFrames.LoadOrStore(streamIdx, struct{}{}); !loaded {
			logger.Debugf(ctx, "[%s] first input frame (stream %d): mediaType=%s pts=%d dts=%d duration=%d size=%d timeBase=%v isKey=%v",
				name, streamIdx, input.GetMediaType(), input.GetPTS(), input.GetDTS(), input.GetDuration(), input.GetSize(), input.GetTimeBase(), input.IsKey())
		}
	}
}

func (t *shouldDebugLogTracker) logFirstOutputPacket(
	ctx context.Context,
	name fmt.Stringer,
	pkt *packetorframe.OutputUnion,
) {
	if pkt.Packet == nil || pkt.Packet.Packet == nil {
		return
	}
	streamIdx := pkt.Packet.GetStreamIndex()
	if _, loaded := t.outputPackets.LoadOrStore(streamIdx, struct{}{}); !loaded {
		logger.Debugf(ctx, "[%s] first output packet (stream %d): mediaType=%s pts=%d dts=%d duration=%d size=%d timeBase=%v isKey=%v",
			name, streamIdx, pkt.Packet.GetMediaType(), pkt.Packet.GetPTS(), pkt.Packet.GetDTS(), pkt.Packet.GetDuration(), pkt.Packet.GetSize(), pkt.Packet.GetTimeBase(), pkt.Packet.IsKey())
	}
}

func (t *shouldDebugLogTracker) logFirstOutputFrame(
	ctx context.Context,
	name fmt.Stringer,
	frm *packetorframe.OutputUnion,
) {
	if frm.Frame == nil || frm.Frame.Frame == nil {
		return
	}
	streamIdx := frm.Frame.GetStreamIndex()
	if _, loaded := t.outputFrames.LoadOrStore(streamIdx, struct{}{}); !loaded {
		logger.Debugf(ctx, "[%s] first output frame (stream %d): mediaType=%s pts=%d dts=%d duration=%d size=%d timeBase=%v isKey=%v",
			name, streamIdx, frm.Frame.GetMediaType(), frm.Frame.GetPTS(), frm.Frame.GetDTS(), frm.Frame.GetDuration(), frm.Frame.GetSize(), frm.Frame.GetTimeBase(), frm.Frame.IsKey())
	}
}
