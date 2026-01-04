// is_key_frame.go implements a condition that checks if a frame is a keyframe.

package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
)

type IsKeyFrame bool

var _ Condition = (IsKeyFrame)(false)

func (v IsKeyFrame) String() string {
	return fmt.Sprintf("IsKeyFrame(%t)", bool(v))
}

func (v IsKeyFrame) Match(
	_ context.Context,
	input frame.Input,
) bool {
	isKeyFrame := input.Flags().Has(astiav.FrameFlagKey)
	return bool(v) == isKeyFrame
}
