// reorder_monotonic_dts.go implements a kernel that reorders packets/frames to ensure monotonic DTS.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/go-ng/container/heap"
	"github.com/go-ng/xsort"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kernelcondition "github.com/xaionaro-go/avpipeline/kernel/condition"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	reorderMonotonicDTSConsiderSource = true
)

type InternalStreamKey struct {
	StreamIndex int
	Source      packetorframe.AbstractSource
}

// inputUnionsByDurationDTS is a min-heap ordering InputUnion items by their
// DTS normalized to time.Duration via each item's own timebase. A plain
// raw-DTS comparison across streams with different timebases (e.g. audio
// at 1/48000 vs video at 1/1000) produces a wrong ordering — raw integers
// in different units are not comparable. Normalizing to Duration gives a
// single time axis on which cross-stream comparisons are meaningful.
//
// Ties on Duration (including the degenerate case where both items share
// a timebase whose denominator is zero, producing Duration=0 for every
// item) break by raw DTS then by stream index. Falling back to raw DTS
// preserves per-stream monotonicity — Duration(dts, tb) is monotonic in
// dts for a fixed tb, so items with equal Duration from the same stream
// must also have equal raw DTS only when they truly coincide; otherwise
// raw DTS gives the correct intra-stream order that sendOneItemFromQueue
// expects when it matches the heap top against the per-stream queue top.
type inputUnionsByDurationDTS []packetorframe.InputUnion

func (s inputUnionsByDurationDTS) Len() int {
	return len(s)
}

func (s inputUnionsByDurationDTS) Less(i, j int) bool {
	di := avconv.Duration(s[i].GetDTS(), s[i].GetTimeBase())
	dj := avconv.Duration(s[j].GetDTS(), s[j].GetTimeBase())
	switch {
	case di != dj:
		return di < dj
	case s[i].GetDTS() != s[j].GetDTS():
		return s[i].GetDTS() < s[j].GetDTS()
	default:
		return s[i].GetStreamIndex() < s[j].GetStreamIndex()
	}
}

func (s inputUnionsByDurationDTS) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// ReorderMonotonicDTS reorders packets/frames to make sure DTS is monotonic across multiple streams
// in the assumption that each stream already produces monotonic DTS.
//
// It works by making a queue of packets/frames from each stream, then:
// When all queues has at least one item, it will gradually send out
// packets/frames from the queues ensuring monotonic DTS until at least
// one queue is empty. Then it will again wait until all queues has at
// least one item; rinse and repeat.
//
// NOT TESTED
type ReorderMonotonicDTS struct {
	*closuresignaler.ClosureSignaler
	Locker                xsync.Gorex // Gorex is not really tested well, so if you suspect corruptions due to concurrency, try replacing this with xsync.Mutex
	ItemQueue             inputUnionsByDurationDTS
	StreamsDTSs           map[InternalStreamKey]*xsort.OrderedAsc[int64]
	MaxDTSDifference      time.Duration
	StartCondition        kernelcondition.Condition[*ReorderMonotonicDTS]
	Started               bool
	PrevDTS               time.Duration
	MaxDTSSeenPerStream      map[InternalStreamKey]time.Duration
	MaxForwardGapPerStream   map[InternalStreamKey]time.Duration
	DiscardUnorderedItems    bool

	emptyQueuesCount         int
	ConditionArgumentNewItem *packetorframe.InputUnion
}

var _ Abstract = (*ReorderMonotonicDTS)(nil)

func NewReorderMonotonicDTS(
	ctx context.Context,
	startCondition kernelcondition.Condition[*ReorderMonotonicDTS],
	maxBufferSize uint,
	maxDTSDifference time.Duration,
	discardUnorderedItems bool,
) *ReorderMonotonicDTS {
	return &ReorderMonotonicDTS{
		ClosureSignaler:       closuresignaler.New(),
		ItemQueue:             make(inputUnionsByDurationDTS, 0, maxBufferSize),
		StreamsDTSs:           make(map[InternalStreamKey]*xsort.OrderedAsc[int64]),
		MaxDTSDifference:      maxDTSDifference,
		StartCondition:        startCondition,
		MaxDTSSeenPerStream:    make(map[InternalStreamKey]time.Duration),
		MaxForwardGapPerStream: make(map[InternalStreamKey]time.Duration),
		DiscardUnorderedItems: discardUnorderedItems,
	}
}

func (r *ReorderMonotonicDTS) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "MonotonicDTS: item: DTS:%d, stream:%d", input.GetDTS(), input.GetStreamIndex())
	defer func() {
		logger.Tracef(ctx, "/MonotonicDTS: item: DTS:%d, stream:%d: %v", input.GetDTS(), input.GetStreamIndex(), _err)
	}()

	clonedInput := input.CloneAsReferencedInput()
	if clonedInput.Get() == nil {
		return kerneltypes.ErrUnexpectedInputType{}
	}

	return xsync.DoA3R1(
		ctx, &r.Locker, r.pushToQueue,
		ctx,
		clonedInput,
		outputCh,
	)
}

// CurrentDTS returns the earliest DTS currently held in the buffer,
// expressed as a time.Duration so that values from streams with
// different timebases (audio 1/48000 vs video 1/1000) remain comparable.
func (r *ReorderMonotonicDTS) CurrentDTS() typing.Optional[time.Duration] {
	if len(r.ItemQueue) == 0 {
		return typing.Optional[time.Duration]{}
	}

	top := r.ItemQueue[0]
	return typing.Opt(avconv.Duration(top.GetDTS(), top.GetTimeBase()))
}

func (r *ReorderMonotonicDTS) pushToQueue(
	ctx context.Context,
	item packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	dts := item.GetDTS()
	// HW decoders (e.g. hevc_cuvid) may emit packets with DTS=NoPtsValue
	// while PTS is valid. Fall back to PTS and update the item so the
	// heap comparison (which reads item.GetDTS()) uses the corrected value.
	if dts == astiav.NoPtsValue {
		dts = item.GetPTS()
		if dts != astiav.NoPtsValue {
			item.SetDTS(dts)
		}
	}
	if dts == astiav.NoPtsValue {
		// Both DTS and PTS absent — cannot be sorted. Send directly,
		// bypassing the reorder buffer.
		logger.Debugf(ctx, "skipping reorder for item with no DTS/PTS on stream %d", item.GetStreamIndex())
		select {
		case outputCh <- item.CloneAsReferencedOutput():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-r.CloseChan():
			return io.EOF
		}
	}
	newItemDTS := avconv.Duration(dts, item.GetTimeBase())

	streamKey := InternalStreamKey{
		StreamIndex: item.GetStreamIndex(),
	}
	if reorderMonotonicDTSConsiderSource {
		streamKey.Source = item.GetSource()
	}

	r.updateStreamDTSReference(ctx, streamKey, newItemDTS)

	if len(r.ItemQueue) >= cap(r.ItemQueue) {
		r.makeRoomInQueue(ctx, outputCh)
	}
	heap.Push(&r.ItemQueue, item)
	top := r.ItemQueue[0]
	logger.Tracef(ctx, "the earliest DTS is now %v (raw %d in timebase %v)",
		avconv.Duration(top.GetDTS(), top.GetTimeBase()), top.GetDTS(), top.GetTimeBase())
	logger.Tracef(ctx, "pushing DTS:%d to stream queue %v", dts, streamKey)
	if _, ok := r.StreamsDTSs[streamKey]; !ok {
		logger.Tracef(ctx, "initializing stream %v", streamKey)
		r.StreamsDTSs[streamKey] = &xsort.OrderedAsc[int64]{}
		r.emptyQueuesCount++
		if len(r.StreamsDTSs) > 100 {
			logger.Errorf(ctx, "too many streams: %d", len(r.StreamsDTSs))
		}
	}
	if len(*r.StreamsDTSs[streamKey]) == 0 {
		logger.Tracef(ctx, "stream %v was previously empty, now it has at least one item", streamKey)
		r.emptyQueuesCount--
	}
	heap.Push(r.StreamsDTSs[streamKey], dts)
	if r.emptyQueuesCount != 0 && !r.Started {
		logger.Tracef(ctx, "not all streams have items yet (emptyQueuesCount: %d), waiting...", r.emptyQueuesCount)
		return
	}

	if !r.Started {
		r.ConditionArgumentNewItem = &item
		if r.StartCondition != nil && !r.StartCondition.Match(ctx, r) {
			logger.Tracef(ctx, "condition %s is not met, not starting yet", r.StartCondition)
			return nil
		}
		logger.Tracef(ctx, "start condition met")
		r.Started = true
	}

	logger.Tracef(ctx, "all %d streams have at least one item in the queue, so it is time to pull something out", len(r.StreamsDTSs))

	if err := r.pullAndSendPendingItems(ctx, outputCh); err != nil {
		return fmt.Errorf("unable to pull&send pending items: %w", err)
	}
	return nil
}

func (r *ReorderMonotonicDTS) EmptyQueuesCount(ctx context.Context) uint {
	return xsync.DoR1(ctx, &r.Locker, func() uint {
		return uint(r.emptyQueuesCount)
	})
}

// updateStreamDTSReference tracks the per-stream DTS high-water mark and
// warns on anomalous DTS jumps (stream restarts, mid-stream consumer joins).
func (r *ReorderMonotonicDTS) updateStreamDTSReference(
	ctx context.Context,
	streamKey InternalStreamKey,
	newItemDTS time.Duration,
) {
	if newItemDTS == avconv.NoDuration {
		return
	}

	reference, hasReference := r.MaxDTSSeenPerStream[streamKey]

	if !hasReference || newItemDTS > reference {
		r.MaxDTSSeenPerStream[streamKey] = newItemDTS
	}

	if !hasReference {
		return
	}

	dtsDiff := newItemDTS - reference
	switch {
	case -dtsDiff > r.MaxDTSDifference:
		// Backward DTS jump within this stream beyond MaxDTSDifference.
		// Reset this stream's high-water mark to the new epoch.
		logger.Warnf(ctx, "ReorderMonotonicDTS: stream %v: new DTS %v is %v behind reference %v (> MaxDTSDifference %v); resetting reference (stream restart)",
			streamKey, newItemDTS, -dtsDiff, reference, r.MaxDTSDifference)
		r.MaxDTSSeenPerStream[streamKey] = newItemDTS
	case dtsDiff > 0:
		// Forward gap — log only when a new per-stream record is set.
		prevMax, hasPrev := r.MaxForwardGapPerStream[streamKey]
		if !hasPrev {
			prevMax = time.Second
			r.MaxForwardGapPerStream[streamKey] = prevMax
		}
		if dtsDiff > prevMax {
			logger.Warnf(ctx, "ReorderMonotonicDTS: stream %v: max forward DTS gap changed from %v to %v (ref=%v, new=%v)",
				streamKey, prevMax, dtsDiff, reference, newItemDTS)
			r.MaxForwardGapPerStream[streamKey] = dtsDiff
		}
	}
}

func (r *ReorderMonotonicDTS) makeRoomInQueue(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) {
	if r.DiscardUnorderedItems {
		logger.Warnf(ctx, "the queue is full, discarding the DTS-oldest item")
		discarded := heap.Pop(&r.ItemQueue)
		discardedKey := InternalStreamKey{StreamIndex: discarded.GetStreamIndex()}
		if reorderMonotonicDTSConsiderSource {
			discardedKey.Source = discarded.GetSource()
		}
		sq := r.StreamsDTSs[discardedKey]
		if sq == nil {
			return
		}
		heap.Pop(sq)
		if len(*sq) == 0 {
			r.emptyQueuesCount++
		}
		return
	}

	logger.Warnf(ctx, "the queue is full, flushing one item from the queue to make space")
	_ = r.sendOneItemFromQueue(ctx, outputCh)
}

func (r *ReorderMonotonicDTS) sendOneItemFromQueue(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "sendOneItemFromQueue")
	defer func() { logger.Tracef(ctx, "/sendOneItemFromQueue: %v", _err) }()

	oldestItem := heap.Pop(&r.ItemQueue)
	streamKey := InternalStreamKey{
		StreamIndex: oldestItem.GetStreamIndex(),
	}
	if reorderMonotonicDTSConsiderSource {
		streamKey.Source = oldestItem.GetSource()
	}
	streamQueue := r.StreamsDTSs[streamKey]
	oldDTS := heap.Pop(streamQueue)
	logger.Tracef(ctx, "popped DTS:%d from stream queue %v", oldDTS, streamKey)

	itemDTS := oldestItem.GetDTS()
	assert(ctx, itemDTS == oldDTS, itemDTS, oldDTS)

	if len(*streamQueue) == 0 {
		logger.Tracef(ctx, "stream %v queue is now empty", streamKey)
		r.emptyQueuesCount++
	}

	if err := r.doSendItem(ctx, oldestItem, outputCh); err != nil {
		return fmt.Errorf("unable to pass along an item (packet or frame): %w", err)
	}

	return nil
}

func (r *ReorderMonotonicDTS) pullAndSendPendingItems(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "pullAndSendPendingItems: emptyQueuesCount=%d", r.emptyQueuesCount)
	defer func() {
		logger.Tracef(ctx, "/pullAndSendPendingItems: emptyQueuesCount=%d: %v", r.emptyQueuesCount, _err)
	}()
	for r.emptyQueuesCount == 0 {
		assert(ctx, len(r.ItemQueue) > 0, len(r.ItemQueue), "if all stream queues are not empty, then obviously the global queue cannot be empty")
		if err := r.sendOneItemFromQueue(ctx, outputCh); err != nil {
			return fmt.Errorf("unable to send one item: %w", err)
		}
	}
	return nil
}

func (r *ReorderMonotonicDTS) doSendItem(
	ctx context.Context,
	item packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "sending out item: DTS:%d, stream:%d", item.GetDTS(), item.GetStreamIndex())
	defer func() {
		logger.Tracef(ctx, "/sending out item: DTS:%d, stream:%d: %v", item.GetDTS(), item.GetStreamIndex(), _err)
	}()
	dts := avconv.Duration(item.GetDTS(), item.GetTimeBase())
	if dts == avconv.NoDuration {
		// Defense-in-depth: pushToQueue should have handled NoPTS via
		// PTS fallback or direct send. If we still get one here, pass
		// through without touching PrevDTS.
		logger.Warnf(ctx, "unexpected NoPTS item in doSendItem (stream %d); passing through", item.GetStreamIndex())
		select {
		case outputCh <- item.CloneAsReferencedOutput():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-r.CloseChan():
			return io.EOF
		}
	}
	if r.PrevDTS > dts {
		switch {
		case r.MaxDTSDifference > 0 && r.PrevDTS-dts > r.MaxDTSDifference:
			// Large backwards jump — likely a stream restart or cross-stream DTS
			// epoch mismatch (e.g. audio retained CLOCK_MONOTONIC timestamps while
			// video was corrected to stream-relative).
			//
			// Intentionally overrides DiscardUnorderedItems: epoch resets must be
			// accepted to restore stream continuity. DiscardUnorderedItems handles
			// small backward jitter, not full epoch mismatches. This is a
			// defense-in-depth measure; the primary fix is in
			// makeTimeMoveOnlyForward which aligns audio/video epochs.
			logger.Warnf(ctx, "DTS went far backwards: previous DTS was %v, now it is %v (%d); resetting reference (stream restart or epoch mismatch)",
				r.PrevDTS, dts, item.GetDTS())
			// Proceed to r.PrevDTS = dts below.
		case r.DiscardUnorderedItems:
			logger.Warnf(ctx, "DTS went backwards: previous DTS was %v, now it is %v (%d); discarding the item",
				r.PrevDTS, dts, item.GetDTS())
			return nil
		default:
			return fmt.Errorf("DTS went backwards: previous DTS was %v, now it is %v (%d)",
				r.PrevDTS, dts, item.GetDTS())
		}
	}
	r.PrevDTS = dts

	select {
	case outputCh <- item.CloneAsReferencedOutput():
	case <-ctx.Done():
		return ctx.Err()
	case <-r.CloseChan():
		return io.EOF
	}
	return nil
}

func (r *ReorderMonotonicDTS) Close(ctx context.Context) error {
	return xsync.DoR1(ctx, &r.Locker, func() error {
		if r.ClosureSignaler.IsClosed() {
			return nil
		}
		r.ClosureSignaler.Close(ctx)
		return nil
	})
}

func (r *ReorderMonotonicDTS) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(r)
}

func (r *ReorderMonotonicDTS) String() string {
	return "ReorderMonotonicDTS"
}

func (r *ReorderMonotonicDTS) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	<-r.ClosureSignaler.CloseChan()
	// flush what's left in the queue
	for len(r.ItemQueue) > 0 {
		if err := r.sendOneItemFromQueue(ctx, outputCh); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReorderMonotonicDTS) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_ret error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _ret) }()
	var errs []error
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		for _, stream := range fmtCtx.Streams() {
			key := InternalStreamKey{
				StreamIndex: stream.Index(),
			}
			if reorderMonotonicDTSConsiderSource {
				key.Source = source
			}
			logger.Debugf(ctx, "making sure stream %d from source %s is initialized", stream.Index(), source)
			_, ok := r.StreamsDTSs[key]
			if !ok {
				r.StreamsDTSs[key] = &xsort.OrderedAsc[int64]{}
				r.emptyQueuesCount++
			}
		}
	})
	logger.Debugf(ctx, "total streams tracked now: %d", len(r.StreamsDTSs))
	return errors.Join(errs...)
}
