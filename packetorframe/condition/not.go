// not.go implements a logical NOT condition.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Not []Condition

var _ Condition = (*Not)(nil)

func (n Not) String() string {
	return fmt.Sprintf("!(%s)", And(n))
}

func (n Not) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	return !And(n).Match(ctx, in)
}
