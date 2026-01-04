// getter_atomic_integer.go implements a getter for atomic integer values.

package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-ng/xatomic"
	"golang.org/x/exp/constraints"
)

type GetterAtomicIntegerT[T constraints.Integer] struct {
	*xatomic.Integer[T]
}

func GetterAtomicInteger[T constraints.Integer](
	integer *xatomic.Integer[T],
) GetterAtomicIntegerT[T] {
	return GetterAtomicIntegerT[T]{Integer: integer}
}

var _ Getter[int32] = GetterAtomicIntegerT[int32]{}

func (g GetterAtomicIntegerT[T]) String() string {
	typeName := fmt.Sprintf("%T", g.Integer.Value)
	typeName = strings.Title(typeName)
	return fmt.Sprintf("Atomic%s(%v)", typeName, g.Integer.Load())
}

func (g GetterAtomicIntegerT[T]) Get(context.Context) T {
	return g.Integer.Load()
}
