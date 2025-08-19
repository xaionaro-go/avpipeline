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
		return input.Frame.KeyFrame()
	}
	isKeyFrame := input.Packet.Flags().Has(astiav.PacketFlagKey)
	return bool(v) == isKeyFrame
}
