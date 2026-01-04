// is_key_frame.go implements a condition that checks if a packet or frame is a key frame.

package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type IsKeyFrame bool

var _ Condition = (IsKeyFrame)(false)

func (v IsKeyFrame) String() string {
	return fmt.Sprintf("IsKeyFrame(%t)", bool(v))
}

func (v IsKeyFrame) Match(
	_ context.Context,
	input packetorframe.InputUnion,
) bool {
	if input.Frame != nil {
		return bool(v) == input.Frame.Flags().Has(astiav.FrameFlagKey)
	}
	if input.Packet == nil {
		return false
	}
	isKeyFrame := input.Packet.Flags().Has(astiav.PacketFlagKey)
	return bool(v) == isKeyFrame
}
