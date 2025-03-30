package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel/types"
)

type Static[K types.Abstract] bool

var _ Condition[types.Abstract] = (Static[types.Abstract])(false)

func (v Static[K]) String() string {
	return fmt.Sprintf("%t", v)
}

func (v Static[K]) Match(context.Context, K) bool {
	return (bool)(v)
}
