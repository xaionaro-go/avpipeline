// is_key_frame.go implements a condition that checks if a packet is a key frame.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
)

type IsKeyFrame bool

var _ Condition = (IsKeyFrame)(false)

func (v IsKeyFrame) String() string {
	return fmt.Sprintf("IsKeyFrame(%t)", bool(v))
}

func (v IsKeyFrame) Match(
	_ context.Context,
	input packet.Input,
) bool {
	isKeyFrame := input.IsKey()
	return bool(v) == isKeyFrame
}
