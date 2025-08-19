package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type MediaType astiav.MediaType

var _ Condition = (MediaType)(0)

func (mt MediaType) String() string {
	return fmt.Sprintf("MediaType(%s)", (astiav.MediaType)(mt))
}

func (mt MediaType) Match(
	ctx context.Context,
	in packetorframe.InputUnion,
) bool {
	return in.GetMediaType() == astiav.MediaType(mt)
}
