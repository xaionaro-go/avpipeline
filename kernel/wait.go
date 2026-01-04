// wait.go implements a kernel that buffers inputs until a condition is met.

package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type Wait struct {
	*closuresignaler.ClosureSignaler
	Locker       xsync.Mutex
	Condition    packetorframecondition.Condition
	Queue        []packetorframe.OutputUnion
	MaxQueueSize uint
}

var _ Abstract = (*Wait)(nil)

func NewWait(
	condition packetorframecondition.Condition,
	maxQueueSize uint,
) *Wait {
	m := &Wait{
		ClosureSignaler: closuresignaler.New(),
		Condition:       condition,
		MaxQueueSize:    maxQueueSize,
	}
	return m
}

func (w *Wait) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	return xsync.DoA3R1(ctx, &w.Locker, w.sendInput, ctx, input, outputCh)
}

func (w *Wait) sendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	output := input.CloneAsReferencedOutput()
	if output.Get() == nil {
		return types.ErrUnexpectedInputType{}
	}

	shouldWait := w.Condition != nil && w.Condition.Match(ctx, input)
	logger.Tracef(ctx, "shouldWait: %t", shouldWait)
	if shouldWait {
		w.Queue = append(w.Queue, output)
		if len(w.Queue) > int(w.MaxQueueSize) {
			w.Queue = w.Queue[1:]
		}
		return nil
	}

	if len(w.Queue) > 0 {
		logger.Debugf(ctx, "releasing %d items", len(w.Queue))
		for _, item := range w.Queue {
			outputCh <- item
		}
		w.Queue = w.Queue[:0]
	}

	outputCh <- output
	return nil
}

func (w *Wait) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(w)
}

func (w *Wait) String() string {
	return fmt.Sprintf("Wait(%v)", w.Condition)
}

func (w *Wait) Close(ctx context.Context) error {
	w.ClosureSignaler.Close(ctx)
	return nil
}

func (w *Wait) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
