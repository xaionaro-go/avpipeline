package condition

import (
	"cmp"
	"context"
	"fmt"

	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type MathCond[T cmp.Ordered] struct {
	ValueGetter   mathcondition.Getter[T]
	MathCondition mathcondition.Condition[T]
}

var _ Condition = (*MathCond[float64])(nil)

func Math[T cmp.Ordered](
	valueGetter mathcondition.Getter[T],
	mathCond mathcondition.Condition[T],
) *MathCond[T] {
	return &MathCond[T]{
		ValueGetter:   valueGetter,
		MathCondition: mathCond,
	}
}

func (c MathCond[T]) String() string {
	return fmt.Sprintf("Math(%s %s)", c.ValueGetter, c.MathCondition)
}

func (c MathCond[T]) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	return c.MathCondition.Match(ctx, c.ValueGetter.Get(ctx))
}
