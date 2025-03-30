package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/container/heap"
	"github.com/go-ng/xsort"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/sort"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

// SyncStreams reorders packets/frames to make sure PTS is monotonic across multiple streams
// in the assumption that each stream already produces monotonic PTS.
//
// It works by making a queue of packets/frames from each stream, then:
// When all queues has at least one item, it will gradually send out
// packets/frames from the queues ensuring monotonic PTS until at least
// one queue is empty. Then it will again wait until all queues has at
// least one item; rinse and repeat.
//
// NOT TESTED
type SyncStreams struct {
	*closeChan
	Locker           xsync.Gorex // Gorex is not really tested well, so if you suspect corruptions due to concurrency, try replacing this with xsync.Mutex
	ItemQueue        sort.InputPacketOrFrames
	StreamsPTSs      map[int]*xsort.OrderedAsc[int64]
	MaxPTSDifference uint64
	StartCondition   condition.Condition[*SyncStreams]
	Started          bool

	emptyQueuesCount         int
	ConditionArgumentNewItem *types.InputPacketOrFrame
}

var _ Abstract = (*SyncStreams)(nil)

func NewSyncStreams(
	ctx context.Context,
	startCondition condition.Condition[*SyncStreams],
	maxBufferSize uint,
	maxPtsDifference uint64,
) *SyncStreams {
	return &SyncStreams{
		closeChan:        newCloseChan(),
		ItemQueue:        make([]types.InputPacketOrFrame, 0, maxBufferSize),
		StreamsPTSs:      make(map[int]*xsort.OrderedAsc[int64]),
		MaxPTSDifference: maxPtsDifference,
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
		types.InputPacketOrFrame{Packet: &packet.Input{
			Packet:        packet.CloneAsReferenced(input.Packet),
			Stream:        input.Stream,
			FormatContext: input.FormatContext,
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
		types.InputPacketOrFrame{Frame: ptr(frame.BuildInput(
			frame.CloneAsReferenced(input.Frame),
			&astiav.CodecContext{},
			input.StreamIndex, input.StreamsCount,
			input.StreamDuration,
			input.TimeBase,
			input.Pos,
			input.Duration,
		))},
		outputPacketsCh, outputFramesCh,
	)
}

func (s *SyncStreams) CurrentPTS() typing.Optional[int64] {
	if len(s.ItemQueue) == 0 {
		return typing.Optional[int64]{}
	}

	return typing.Opt(s.ItemQueue[0].GetPTS())
}

func (s *SyncStreams) pushToQueue(
	ctx context.Context,
	item types.InputPacketOrFrame,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	pts := item.GetPTS()
	shouldContinue, err := s.enforceLowPTSDifference(ctx, pts, outputPacketsCh, outputFramesCh)
	if err != nil {
		return fmt.Errorf("unable to enforce low enough PTS difference: %w", err)
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

	streamIndex := item.GetStreamIndex()
	if _, ok := s.StreamsPTSs[streamIndex]; !ok {
		s.StreamsPTSs[streamIndex] = &xsort.OrderedAsc[int64]{}
		s.emptyQueuesCount++
	}
	if len(*s.StreamsPTSs[streamIndex]) == 0 {
		s.emptyQueuesCount--
	}
	heap.Push(s.StreamsPTSs[streamIndex], pts)
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

func (s *SyncStreams) enforceLowPTSDifference(
	ctx context.Context,
	newItemPTS int64,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (bool, error) {
	currentPTSOptional := s.CurrentPTS()
	if !currentPTSOptional.IsSet() {
		return true, nil
	}

	currentPTS := currentPTSOptional.Get()
	ptsDiff := int64(newItemPTS) - int64(currentPTS)
	switch {
	case -ptsDiff > int64(s.MaxPTSDifference):
		logger.Warnf(ctx, "received too old item (packet or frame): PTS:%d is lesser than %d-%d; discarding it", newItemPTS, currentPTS, s.MaxPTSDifference)
		return false, nil
	case ptsDiff > int64(s.MaxPTSDifference):
		logger.Warnf(ctx, "received an item way newer than previously known items (packets or/and frames): PTS %d is greater than %d+%d; submitting old items", newItemPTS, currentPTS, s.MaxPTSDifference)
		for {
			currentPTS := s.CurrentPTS()
			if !currentPTS.IsSet() {
				break
			}
			if currentPTS.Get()+int64(s.MaxPTSDifference) >= newItemPTS {
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
	streamQueue := s.StreamsPTSs[oldestItem.GetStreamIndex()]
	oldPTS := heap.Pop(streamQueue)

	itemPTS := oldestItem.GetPTS()
	assert(ctx, itemPTS == oldPTS, itemPTS, oldPTS)

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
	item types.InputPacketOrFrame,
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
		if s.closeChan.IsClosed() {
			return nil
		}
		s.closeChan.Close(ctx)
		return nil
	})
}

func (s *SyncStreams) String() string {
	return "SyncStream"
}

func (s *SyncStreams) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	<-s.closeChan.CloseChan()
	// flush what's left in the queue
	for len(s.ItemQueue) > 0 {
		if err := s.sendOneItemFromQueue(ctx, outputPacketsCh, outputFramesCh); err != nil {
			return err
		}
	}
	return nil
}
