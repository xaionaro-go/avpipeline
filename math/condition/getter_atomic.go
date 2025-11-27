package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-ng/xatomic"
	"golang.org/x/exp/constraints"
)

type GetterAtomic[T constraints.Integer] struct {
	*xatomic.Integer[T]
}

var _ Getter[int32] = GetterAtomic[int32]{}

func (g GetterAtomic[T]) String() string {
	typeName := fmt.Sprintf("%T", g.Integer.Value)
	typeName = strings.Title(typeName)
	return fmt.Sprintf("Atomic%s(%v)", typeName, g.Integer.Load())
}

func (g GetterAtomic[T]) Get(context.Context) T {
	return g.Integer.Load()
}
