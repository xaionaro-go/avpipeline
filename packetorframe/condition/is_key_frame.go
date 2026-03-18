// is_key_frame.go implements a condition that checks if a packet or frame is a key frame.

package condition

import (
	"context"
	"fmt"

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
	// Use the nil-safe IsKey() methods instead of Flags().Has() to avoid
	// nil dereferences when Frame or Packet is nil inside the union member.
	if input.Frame != nil {
		return bool(v) == input.Frame.IsKey()
	}
	if input.Packet == nil {
		return false
	}
	return bool(v) == input.Packet.IsKey()
}
