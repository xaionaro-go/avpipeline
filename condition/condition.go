package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Condition interface {
	fmt.Stringer
	Match(context.Context, types.InputPacket) bool
}
