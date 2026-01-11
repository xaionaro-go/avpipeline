// contains_filler.go implements a condition that checks if a packet is a filler packet.

package condition

import (
	"context"
	"fmt"

	extradatapacket "github.com/xaionaro-go/avpipeline/extradata/packet"
	"github.com/xaionaro-go/avpipeline/packet"
)

type ContainsFiller bool

var _ Condition = ContainsFiller(false)

func (v ContainsFiller) String() string {
	return fmt.Sprintf("ContainsFiller(%t)", bool(v))
}

func (v ContainsFiller) Match(
	_ context.Context,
	input packet.Input,
) bool {
	return bool(v) == containsFiller(input)
}

func containsFiller(input packet.Input) bool {
	cp := input.GetCodecParameters()
	if cp == nil {
		return false
	}

	data := input.Data()
	if len(data) == 0 {
		return false
	}

	count := 0
	fillerCount := 0
	for nalu, err := range extradatapacket.Iter(cp.CodecID(), data) {
		if err != nil {
			return false
		}
		count++
		if extradatapacket.IsFiller(cp.CodecID(), nalu.Type) {
			fillerCount++
		}
	}

	return count > 0 && count == fillerCount
}
