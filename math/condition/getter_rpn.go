package condition

import (
	"fmt"

	"github.com/xaionaro-go/rpn"
	rpntypes "github.com/xaionaro-go/rpn/types"
)

type GetterRPN[A any, T rpntypes.Number] struct {
	Expr rpn.Expr[A, T]
	Arg  A
}

var _ Getter[float64] = GetterRPN[any, float64]{}

func (g GetterRPN[A, T]) String() string {
	if g.Expr == nil {
		var zeroValue T
		return fmt.Sprintf("RPN(<invalid> => %v)", zeroValue)
	}
	return fmt.Sprintf("RPN(%s)", g.Expr)
}

func (g GetterRPN[A, T]) Get() T {
	if g.Expr == nil {
		var zeroValue T
		return zeroValue
	}

	return g.Expr.Eval(g.Arg)
}
