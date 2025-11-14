package kernel

import (
	"context"
	"fmt"

	"github.com/go-ng/container/heap"
	"github.com/go-ng/xsort"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/condition"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/sort"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

const (
	syncStreamsConsiderSource = true
)

type InternalStreamKey struct {
	StreamIndex int
	Source      packet.Source
}

// SyncStreams reorders packets/frames to make sure DTS is monotonic across multiple streams
// in the assumption that each stream already produces monotonic DTS.
//
// It works by making a queue of packets/frames from each stream, then:
// When all queues has at least one item, it will gradually send out
// packets/frames from the queues ensuring monotonic DTS until at least
// one queue is empty. Then it will again wait until all queues has at
// least one item; rinse and repeat.
//
// NOT TESTED
type SyncStreams struct {
	*closuresignaler.ClosureSignaler
	Locker           xsync.Gorex // Gorex is not really tested well, so if you suspect corruptions due to concurrency, try replacing this with xsync.Mutex
	ItemQueue        sort.InputPacketOrFrameUnions
	StreamsDTSs      map[InternalStreamKey]*xsort.OrderedAsc[int64]
	MaxDTSDifference uint64
	StartCondition   condition.Condition[*SyncStreams]
	Started          bool

	emptyQueuesCount         int
	ConditionArgumentNewItem *packetorframe.InputUnion
}

var _ Abstract = (*SyncStreams)(nil)

func NewMonotonicDTS(
	ctx context.Context,
	startCondition condition.Condition[*SyncStreams],
	maxBufferSize uint,
	maxDTSDifference uint64,
) *SyncStreams {
	return &SyncStreams{
		ClosureSignaler:  closuresignaler.New(),
		ItemQueue:        make(sort.InputPacketOrFrameUnions, 0, maxBufferSize),
		StreamsDTSs:      make(map[InternalStreamKey]*xsort.OrderedAsc[int64]),
		MaxDTSDifference: maxDTSDifference,
		StartCondition:   startCondition,
		Started:          false,
	}
}

func (s *SyncStreams) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(
		ctx, &s.Locker, s.pushToQueue,
		ctx,
		packetorframe.InputUnion{Packet: &packet.Input{
			Packet:     packet.CloneAsReferenced(input.Packet),
			StreamInfo: input.StreamInfo,
		}},
		outputPacketsCh, outputFramesCh,
	)
}

func (s *SyncStreams) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	return xsync.DoA4R1(
		ctx, &s.Locker, s.pushToQueue,
		ctx,
		packetorframe.InputUnion{Frame: ptr(frame.BuildInput(
			frame.CloneAsReferenced(input.Frame),
			input.Pos,
			input.StreamInfo,
		))},
		outputPacketsCh, outputFramesCh,
	)
}

func (s *SyncStreams) CurrentDTS() typing.Optional[int64] {
	if len(s.ItemQueue) == 0 {
		return typing.Optional[int64]{}
	}

	return typing.Opt(s.ItemQueue[0].Packet.Dts())
}

func (s *SyncStreams) pushToQueue(
	ctx context.Context,
	item packetorframe.InputUnion,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	dts := item.Packet.Dts()
	shouldContinue, err := s.enforceLowDTSDifference(ctx, dts, outputPacketsCh, outputFramesCh)
	if err != nil {
		return fmt.Errorf("unable to enforce low enough DTS difference: %w", err)
	}
	if !shouldContinue {
		logger.Debugf(ctx, "skipping the item")
		return nil
	}

	if len(s.ItemQueue) >= cap(s.ItemQueue) {
		logger.Warnf(ctx, "the queue is full, flushing one item from the queue to make space")
		if err := s.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
			return nil
		}
	}
	heap.Push(&s.ItemQueue, item)

	streamKey := InternalStreamKey{
		StreamIndex: item.GetStreamIndex(),
	}
	if syncStreamsConsiderSource && item.Packet != nil {
		streamKey.Source = item.Packet.Source
	}
	if _, ok := s.StreamsDTSs[streamKey]; !ok {
		s.StreamsDTSs[streamKey] = &xsort.OrderedAsc[int64]{}
		s.emptyQueuesCount++
		if len(s.StreamsDTSs) > 100 {
			logger.Errorf(ctx, "too many streams: %d", len(s.StreamsDTSs))
		}
	}
	if len(*s.StreamsDTSs[streamKey]) == 0 {
		s.emptyQueuesCount--
	}
	heap.Push(s.StreamsDTSs[streamKey], dts)
	if s.emptyQueuesCount != 0 {
		return
	}

	if !s.Started {
		s.ConditionArgumentNewItem = &item
		if s.StartCondition != nil && !s.StartCondition.Match(ctx, s) {
			logger.Tracef(ctx, "condition %s is not met, not starting yet", s.StartCondition)
			return nil
		}
		s.Started = true
	}

	if err := s.pullAndSendPendingItems(ctx, outputPacketsCh, outputFramesCh); err != nil {
		return fmt.Errorf("unable to pull&send pending items: %w", err)
	}
	return nil
}

func (s *SyncStreams) EmptyQueuesCount(ctx context.Context) uint {
	return xsync.DoR1(ctx, &s.Locker, func() uint {
		return uint(s.emptyQueuesCount)
	})
}

func (s *SyncStreams) enforceLowDTSDifference(
	ctx context.Context,
	newItemDTS int64,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (bool, error) {
	currentDTSOptional := s.CurrentDTS()
	if !currentDTSOptional.IsSet() {
		return true, nil
	}

	currentDTS := currentDTSOptional.Get()
	dtsDiff := int64(newItemDTS) - int64(currentDTS)
	switch {
	case -dtsDiff > int64(s.MaxDTSDifference):
		logger.Warnf(ctx, "received too old item (packet or frame): DTS:%d is lesser than %d-%d; discarding it", newItemDTS, currentDTS, s.MaxDTSDifference)
		return false, nil
	case dtsDiff > int64(s.MaxDTSDifference):
		logger.Warnf(ctx, "received an item way newer than previously known items (packets or/and frames): DTS %d is greater than %d+%d; submitting old items", newItemDTS, currentDTS, s.MaxDTSDifference)
		for {
			currentDTS := s.CurrentDTS()
			if !currentDTS.IsSet() {
				break
			}
			if currentDTS.Get()+int64(s.MaxDTSDifference) >= newItemDTS {
				break
			}

			if err := s.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
				return false, err
			}

		}
	}

	return true, nil
}

func (s *SyncStreams) sendOneItemFromQueue(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	oldestItem := heap.Pop(&s.ItemQueue)
	streamKey := InternalStreamKey{
		StreamIndex: oldestItem.GetStreamIndex(),
	}
	if syncStreamsConsiderSource && oldestItem.Packet != nil {
		streamKey.Source = oldestItem.Packet.Source
	}
	streamQueue := s.StreamsDTSs[streamKey]
	oldDTS := heap.Pop(streamQueue)

	itemDTS := oldestItem.Packet.Dts()
	assert(ctx, itemDTS == oldDTS, itemDTS, oldDTS)

	if len(*streamQueue) == 0 {
		s.emptyQueuesCount++
	}

	if err := s.doSendItem(ctx, oldestItem, outputPacketsCh, outputFramesCh); err != nil {
		return fmt.Errorf("unable to pass along an item (packet or frame): %w", err)
	}

	return nil
}

func (s *SyncStreams) pullAndSendPendingItems(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	for s.emptyQueuesCount == 0 {
		assert(ctx, len(s.ItemQueue) > 0, len(s.ItemQueue), "if all stream queues are not empty, then obviously the global queue cannot be empty")
		if err := s.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
			return fmt.Errorf("unable to send one item: %w", err)
		}
	}
	return nil
}

func (s *SyncStreams) doSendItem(
	ctx context.Context,
	item packetorframe.InputUnion,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if item.Frame != nil {
		outputFramesCh <- frame.Output(*item.Frame)
		return nil
	}

	outputPacketsCh <- packet.Output(*item.Packet)
	return nil
}

func (s *SyncStreams) Close(ctx context.Context) error {
	return xsync.DoR1(ctx, &s.Locker, func() error {
		if s.ClosureSignaler.IsClosed() {
			return nil
		}
		s.ClosureSignaler.Close(ctx)
		return nil
	})
}

func (s *SyncStreams) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(s)
}

func (s *SyncStreams) String() string {
	return "SyncStream"
}

func (s *SyncStreams) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	<-s.ClosureSignaler.CloseChan()
	// flush what's left in the queue
	for len(s.ItemQueue) > 0 {
		if err := s.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
			return err
		}
	}
	return nil
}
