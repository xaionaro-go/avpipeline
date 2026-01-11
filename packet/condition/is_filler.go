// is_filler.go implements a condition that checks if a packet is a filler packet.

package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/packet"
)

type IsFiller bool

var _ Condition = IsFiller(false)

func (v IsFiller) String() string {
	return fmt.Sprintf("IsFiller(%t)", bool(v))
}

func (v IsFiller) Match(
	_ context.Context,
	input packet.Input,
) bool {
	return bool(v) == isFillerPacket(input)
}

func isFillerPacket(input packet.Input) bool {
	cp := input.GetCodecParameters()
	if cp == nil {
		return false
	}

	data := input.Data()
	if len(data) == 0 {
		return false
	}

	switch cp.CodecID() {
	case astiav.CodecIDH264:
		h264Seq, err := extradata.ParseH264AnnexB(data)
		if err != nil {
			return false
		}
		if len(h264Seq.NALUs) == 0 {
			return false
		}
		for _, nalu := range h264Seq.NALUs {
			if nalu.Type != extradata.H264NalUnitTypeFiller {
				return false
			}
		}
		return true

	case astiav.CodecIDHevc:
		h265Seq, err := extradata.ParseH265AnnexB(data)
		if err != nil {
			return false
		}
		if len(h265Seq.NALUs) == 0 {
			return false
		}
		for _, nalu := range h265Seq.NALUs {
			if nalu.Type != extradata.H265NalUnitTypeFD {
				return false
			}
		}
		return true
	}

	return false
}
