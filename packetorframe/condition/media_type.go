// media_type.go implements a condition that matches the media type.

package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type MediaType astiav.MediaType

var _ Condition = MediaType(0)

func (v MediaType) String() string {
	return fmt.Sprintf("MediaType(%s)", astiav.MediaType(v))
}

func (v MediaType) Match(ctx context.Context, in packetorframe.InputUnion) bool {
	return in.GetMediaType() == astiav.MediaType(v)
}
