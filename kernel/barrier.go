// barrier.go implements a kernel that can block or drop packets based on a condition.

package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/types"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

var _ Abstract = (*Barrier)(nil)
var _ kerneltypes.Resetter = (*Barrier)(nil)

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

// Reset clears any per-chain observation state held by the wrapped
// Condition (StateGetter). Called by chain-restart paths so that
// downstream Barriers re-establish PTS continuity / pending state
// against the freshly-opened upstream — without this, a SwitchOutput's
// ptsBridge keeps the prior connection's lastEmitted PTS for the same
// chainID and applies a stale offset (or none at all) to packets from
// the new connection, blocking video flow.
func (b *barrierHandler) Reset(ctx context.Context) error {
	if r, ok := b.Condition.(kerneltypes.Resetter); ok {
		return r.Reset(ctx)
	}
	return nil
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
			logger.Tracef(ctx, "Barrier[%p] blocking on state change", b)
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
