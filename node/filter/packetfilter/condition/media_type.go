package condition

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type MediaType astiav.MediaType

var _ Condition = MediaType(0)

func (v MediaType) String() string {
	return fmt.Sprintf("MediaType(%s)", astiav.MediaType(v))
}

func (v MediaType) Match(ctx context.Context, in Input) bool {
	return in.Input.GetMediaType() == astiav.MediaType(v)
}
