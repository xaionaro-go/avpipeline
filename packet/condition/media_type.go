// media_type.go implements a condition that checks the media type of a packet.

package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
)

type MediaType astiav.MediaType

var _ Condition = (MediaType)(0)

func (mt MediaType) String() string {
	return fmt.Sprintf("MediaType(%s)", (astiav.MediaType)(mt))
}

func (mt MediaType) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return pkt.GetCodecParameters().MediaType() == astiav.MediaType(mt)
}
