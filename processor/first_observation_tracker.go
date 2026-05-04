// first_observation_tracker.go records "first observation" facts for
// a processor: per-stream first-input/output debug-log dedup AND a
// single first-ever output unix-nanosecond timestamp.

package processor

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/xsync"
)

// firstObservationTracker records "first observation" facts for the
// processor it is embedded in:
//   - per-stream first-packet/first-frame logging dedup (so debug
//     logs emit one line per (direction, type, streamIndex) instead
//     of spamming every packet);
//   - first-ever output unix-nanosecond timestamp (write-once across
//     ALL streams) for stats RPC consumers — used by
//     ffstreamctl stats first-frame to walk the pipeline graph and
//     identify the exact node where flow stalled.
//
// Both responsibilities observe the same event (first packet/frame
// out), so consolidating them here avoids parallel redundant calls
// from the hot-path forwarder loop.
type firstObservationTracker struct {
	inputPackets  xsync.Map[int, struct{}]
	inputFrames   xsync.Map[int, struct{}]
	outputPackets xsync.Map[int, struct{}]
	outputFrames  xsync.Map[int, struct{}]

	// firstOutputUnixNano is the unix-nanosecond timestamp at which
	// the FIRST output packet OR frame was observed. Set once via
	// CompareAndSwap from zero — subsequent first-observations on
	// other streams do not re-write the field. Zero means "no
	// output observed yet".
	firstOutputUnixNano atomic.Int64
}

// recordFirstOutputTimestamp performs a one-shot CompareAndSwap from
// zero to time.Now().UnixNano(). The Load short-circuits the common
// steady-state path so we avoid the time.Now() syscall once the
// timestamp has been set.
func (t *firstObservationTracker) recordFirstOutputTimestamp() {
	if t.firstOutputUnixNano.Load() != 0 {
		return
	}
	t.firstOutputUnixNano.CompareAndSwap(0, time.Now().UnixNano())
}

// FirstOutputUnixNano returns the unix-nanosecond timestamp at which
// the first output packet/frame was observed, or 0 if no output has
// been seen yet.
func (t *firstObservationTracker) FirstOutputUnixNano() int64 {
	return t.firstOutputUnixNano.Load()
}

func (t *firstObservationTracker) logFirstInput(
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

func (t *firstObservationTracker) logFirstOutputPacket(
	ctx context.Context,
	name fmt.Stringer,
	pkt *packetorframe.OutputUnion,
) {
	if pkt.Packet == nil {
		return
	}
	// Record the first-output timestamp for stats RPC consumers.
	// Cheap fast-path Load skips the CAS once already set; covers
	// every output regardless of whether the inner astiav.Packet is
	// nil (which is valid for some test/synthetic flows).
	t.recordFirstOutputTimestamp()
	if pkt.Packet.Packet == nil {
		return
	}
	streamIdx := pkt.Packet.GetStreamIndex()
	if _, loaded := t.outputPackets.LoadOrStore(streamIdx, struct{}{}); !loaded {
		logger.Debugf(ctx, "[%s] first output packet (stream %d): mediaType=%s pts=%d dts=%d duration=%d size=%d timeBase=%v isKey=%v",
			name, streamIdx, pkt.Packet.GetMediaType(), pkt.Packet.GetPTS(), pkt.Packet.GetDTS(), pkt.Packet.GetDuration(), pkt.Packet.GetSize(), pkt.Packet.GetTimeBase(), pkt.Packet.IsKey())
	}
}

func (t *firstObservationTracker) logFirstOutputFrame(
	ctx context.Context,
	name fmt.Stringer,
	frm *packetorframe.OutputUnion,
) {
	if frm.Frame == nil {
		return
	}
	t.recordFirstOutputTimestamp()
	if frm.Frame.Frame == nil {
		return
	}
	streamIdx := frm.Frame.GetStreamIndex()
	if _, loaded := t.outputFrames.LoadOrStore(streamIdx, struct{}{}); !loaded {
		logger.Debugf(ctx, "[%s] first output frame (stream %d): mediaType=%s pts=%d dts=%d duration=%d size=%d timeBase=%v isKey=%v",
			name, streamIdx, frm.Frame.GetMediaType(), frm.Frame.GetPTS(), frm.Frame.GetDTS(), frm.Frame.GetDuration(), frm.Frame.GetSize(), frm.Frame.GetTimeBase(), frm.Frame.IsKey())
	}
}
