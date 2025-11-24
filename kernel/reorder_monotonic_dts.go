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
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kernelcondition "github.com/xaionaro-go/avpipeline/kernel/condition"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/sort"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	reorderMonotonicDTSConsiderSource = true
)

type InternalStreamKey struct {
	StreamIndex int
	Source      packet.Source
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
	ItemQueue             sort.InputPacketOrFrameUnionsByDTS
	StreamsDTSs           map[InternalStreamKey]*xsort.OrderedAsc[int64]
	MaxDTSDifference      uint64
	StartCondition        kernelcondition.Condition[*ReorderMonotonicDTS]
	Started               bool
	PrevDTS               time.Duration
	DiscardUnorderedItems bool

	emptyQueuesCount         int
	ConditionArgumentNewItem *packetorframe.InputUnion
}

var _ Abstract = (*ReorderMonotonicDTS)(nil)

func NewReorderMonotonicDTS(
	ctx context.Context,
	startCondition kernelcondition.Condition[*ReorderMonotonicDTS],
	maxBufferSize uint,
	maxDTSDifference uint64,
	discardUnorderedItems bool,
) *ReorderMonotonicDTS {
	return &ReorderMonotonicDTS{
		ClosureSignaler:       closuresignaler.New(),
		ItemQueue:             make(sort.InputPacketOrFrameUnionsByDTS, 0, maxBufferSize),
		StreamsDTSs:           make(map[InternalStreamKey]*xsort.OrderedAsc[int64]),
		MaxDTSDifference:      maxDTSDifference,
		StartCondition:        startCondition,
		DiscardUnorderedItems: discardUnorderedItems,
	}
}

func (r *ReorderMonotonicDTS) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "MonotonicDTS: packet: DTS:%d, stream:%d", input.Packet.Dts(), input.StreamInfo.Index)
	defer func() {
		logger.Tracef(ctx, "/MonotonicDTS: packet: DTS:%d, stream:%d: %v", input.Packet.Dts(), input.StreamInfo.Index, _err)
	}()
	return xsync.DoA4R1(
		ctx, &r.Locker, r.pushToQueue,
		ctx,
		packetorframe.InputUnion{Packet: &packet.Input{
			Packet:     packet.CloneAsReferenced(input.Packet),
			StreamInfo: input.StreamInfo,
		}},
		outputPacketsCh, outputFramesCh,
	)
}

func (r *ReorderMonotonicDTS) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "MonotonicDTS: frame: DTS:%d, stream:%d", input.Frame.PktDts(), input.StreamInfo.StreamIndex)
	defer func() {
		logger.Tracef(ctx, "/MonotonicDTS: frame: DTS:%d, stream:%d: %v", input.Frame.PktDts(), input.StreamInfo.StreamIndex, _err)
	}()
	return xsync.DoA4R1(
		ctx, &r.Locker, r.pushToQueue,
		ctx,
		packetorframe.InputUnion{Frame: ptr(frame.BuildInput(
			frame.CloneAsReferenced(input.Frame),
			input.Pos,
			input.StreamInfo,
		))},
		outputPacketsCh, outputFramesCh,
	)
}

func (r *ReorderMonotonicDTS) CurrentDTS() typing.Optional[int64] {
	if len(r.ItemQueue) == 0 {
		return typing.Optional[int64]{}
	}

	return typing.Opt(r.ItemQueue[0].Packet.Dts())
}

func (r *ReorderMonotonicDTS) pushToQueue(
	ctx context.Context,
	item packetorframe.InputUnion,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	dts := item.Packet.Dts()
	shouldContinue, err := r.enforceLowDTSDifference(ctx, dts, outputPacketsCh, outputFramesCh)
	if err != nil {
		return fmt.Errorf("unable to enforce low enough DTS difference: %w", err)
	}
	if !shouldContinue {
		logger.Debugf(ctx, "skipping the item")
		return nil
	}

	if len(r.ItemQueue) >= cap(r.ItemQueue) {
		if r.DiscardUnorderedItems {
			logger.Warnf(ctx, "the queue is full, discarding the DTS-oldest item")
			heap.Pop(&r.ItemQueue)
		} else {
			logger.Warnf(ctx, "the queue is full, flushing one item from the queue to make space")
			if err := r.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
				return nil
			}
		}
	}
	heap.Push(&r.ItemQueue, item)
	logger.Tracef(ctx, "the earliest DTS is now %d", r.ItemQueue[0].Packet.Dts())

	streamKey := InternalStreamKey{
		StreamIndex: item.GetStreamIndex(),
	}
	if reorderMonotonicDTSConsiderSource && item.Packet != nil {
		streamKey.Source = item.Packet.GetSource()
	}
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
	if r.emptyQueuesCount != 0 {
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

	if err := r.pullAndSendPendingItems(ctx, outputPacketsCh, outputFramesCh); err != nil {
		return fmt.Errorf("unable to pull&send pending items: %w", err)
	}
	return nil
}

func (r *ReorderMonotonicDTS) EmptyQueuesCount(ctx context.Context) uint {
	return xsync.DoR1(ctx, &r.Locker, func() uint {
		return uint(r.emptyQueuesCount)
	})
}

func (r *ReorderMonotonicDTS) enforceLowDTSDifference(
	ctx context.Context,
	newItemDTS int64,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (bool, error) {
	currentDTSOptional := r.CurrentDTS()
	if !currentDTSOptional.IsSet() {
		return true, nil
	}

	currentDTS := currentDTSOptional.Get()
	dtsDiff := int64(newItemDTS) - int64(currentDTS)
	switch {
	case -dtsDiff > int64(r.MaxDTSDifference):
		logger.Warnf(ctx, "received too old item (packet or frame): DTS:%d is lesser than %d-%d; discarding it", newItemDTS, currentDTS, r.MaxDTSDifference)
		return false, nil
	case dtsDiff > int64(r.MaxDTSDifference):
		logger.Warnf(ctx, "received an item way newer than previously known items (packets or/and frames): DTS %d is greater than %d+%d; submitting old items", newItemDTS, currentDTS, r.MaxDTSDifference)
		for {
			currentDTS := r.CurrentDTS()
			if !currentDTS.IsSet() {
				break
			}
			if currentDTS.Get()+int64(r.MaxDTSDifference) >= newItemDTS {
				break
			}

			if err := r.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
				return false, err
			}

		}
	}

	return true, nil
}

func (r *ReorderMonotonicDTS) sendOneItemFromQueue(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "sendOneItemFromQueue")
	defer func() { logger.Tracef(ctx, "/sendOneItemFromQueue: %v", _err) }()

	oldestItem := heap.Pop(&r.ItemQueue)
	streamKey := InternalStreamKey{
		StreamIndex: oldestItem.GetStreamIndex(),
	}
	if reorderMonotonicDTSConsiderSource && oldestItem.Packet != nil {
		streamKey.Source = oldestItem.Packet.Source
	}
	streamQueue := r.StreamsDTSs[streamKey]
	oldDTS := heap.Pop(streamQueue)
	logger.Tracef(ctx, "popped DTS:%d from stream queue %v", oldDTS, streamKey)

	itemDTS := oldestItem.Packet.Dts()
	assert(ctx, itemDTS == oldDTS, itemDTS, oldDTS)

	if len(*streamQueue) == 0 {
		logger.Tracef(ctx, "stream %v queue is now empty", streamKey)
		r.emptyQueuesCount++
	}

	if err := r.doSendItem(ctx, oldestItem, outputPacketsCh, outputFramesCh); err != nil {
		return fmt.Errorf("unable to pass along an item (packet or frame): %w", err)
	}

	return nil
}

func (r *ReorderMonotonicDTS) pullAndSendPendingItems(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "pullAndSendPendingItems: emptyQueuesCount=%d", r.emptyQueuesCount)
	defer func() {
		logger.Tracef(ctx, "/pullAndSendPendingItems: emptyQueuesCount=%d: %v", r.emptyQueuesCount, _err)
	}()
	for r.emptyQueuesCount == 0 {
		assert(ctx, len(r.ItemQueue) > 0, len(r.ItemQueue), "if all stream queues are not empty, then obviously the global queue cannot be empty")
		if err := r.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
			return fmt.Errorf("unable to send one item: %w", err)
		}
	}
	return nil
}

func (r *ReorderMonotonicDTS) doSendItem(
	ctx context.Context,
	item packetorframe.InputUnion,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "sending out item: DTS:%d, stream:%d", item.Packet.Dts(), item.GetStreamIndex())
	defer func() {
		logger.Tracef(ctx, "/sending out item: DTS:%d, stream:%d: %v", item.Packet.Dts(), item.GetStreamIndex(), _err)
	}()
	dts := avconv.Duration(item.GetDTS(), item.GetTimeBase())
	if r.PrevDTS > dts {
		if r.DiscardUnorderedItems {
			logger.Warnf(ctx, "DTS went backwards: previous DTS was %v, now it is %v (%d); discarding the item", r.PrevDTS, dts, item.GetDTS())
			return nil
		}
		return fmt.Errorf("DTS went backwards: previous DTS was %v, now it is %v (%d)", r.PrevDTS, dts, item.GetDTS())
	}
	r.PrevDTS = dts
	if item.Frame != nil {
		select {
		case outputFramesCh <- frame.Output(*item.Frame):
		case <-ctx.Done():
			return ctx.Err()
		case <-r.CloseChan():
			return io.EOF
		}
		return nil
	}

	select {
	case outputPacketsCh <- packet.Output(*item.Packet):
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
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	<-r.ClosureSignaler.CloseChan()
	// flush what's left in the queue
	for len(r.ItemQueue) > 0 {
		if err := r.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
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
