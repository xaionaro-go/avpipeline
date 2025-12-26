package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
)

type HasPacketFlags astiav.PacketFlags

var _ Condition = (IsKeyFrame)(false)

func (v HasPacketFlags) String() string {
	return fmt.Sprintf("HasPacketFlags(0x%X)", astiav.PacketFlags(v))
}

func (v HasPacketFlags) Match(
	_ context.Context,
	input packet.Input,
) bool {
	if input.Packet == nil {
		return false
	}
	return input.Packet.Flags().Has(astiav.PacketFlag(v))
}
