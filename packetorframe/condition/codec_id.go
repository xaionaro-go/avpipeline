// codec_id.go implements a condition that matches the codec ID.

package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type CodecID astiav.CodecID

var _ Condition = CodecID(0)

func (v CodecID) String() string {
	return fmt.Sprintf("CodecID(%s)", astiav.CodecID(v))
}

func (v CodecID) Match(ctx context.Context, in packetorframe.InputUnion) bool {
	if in.GetCodecParameters() == nil {
		return false
	}
	return in.GetCodecParameters().CodecID() == astiav.CodecID(v)
}
