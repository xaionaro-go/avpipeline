// barrier.go implements a kernel that can block or drop packets based on a condition.

package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

var _ Abstract = (*Barrier)(nil)

type Barrier = boilerplate.Base[*barrierHandler]

func NewBarrier(ctx context.Context, cond stategetter.StateGetter) *Barrier {
	h := newBarrierHandler(cond)
	return boilerplate.NewBasicKernel(ctx, h)
}

type barrierHandler struct {
	Condition stategetter.StateGetter
}

var _ boilerplate.VisitInputer = (*barrierHandler)(nil)

func newBarrierHandler(
	cond stategetter.StateGetter, // is never nil: all callers pass non-nil values
) *barrierHandler {
	return &barrierHandler{
		Condition: cond,
	}
}

func (b *barrierHandler) String() string {
	return fmt.Sprintf("Barrier(%s)", b.Condition)
}

func (b *barrierHandler) VisitInput(
	ctx context.Context,
	input *packetorframe.InputUnion,
) (_err error) {
	logger.Tracef(ctx, "VisitInput")
	defer func() { logger.Tracef(ctx, "/VisitInput: %v", _err) }()
	return b.processInput(ctx, *input)
}

func (b *barrierHandler) processInput(
	ctx context.Context,
	input packetorframe.InputUnion,
) (err error) {
	for {
		state, changeCh := b.Condition.GetState(ctx, input)
		switch state {
		case types.StatePass:
			return nil
		case types.StateBlock:
			select {
			case <-changeCh:
			case <-ctx.Done():
				return ctx.Err()
			}
		case types.StateDrop:
			return boilerplate.ErrSkip{}
		default:
			return fmt.Errorf("unexpected barrier state: %v", state)
		}
	}
}
