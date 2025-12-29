package condition

import (
	"context"

	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
)

type MediaType packetorframecondition.MediaType

var _ Condition = MediaType(0)

func (v MediaType) String() string {
	return packetorframecondition.MediaType(v).String()
}

func (v MediaType) Match(ctx context.Context, in Input) bool {
	return packetorframecondition.MediaType(v).Match(ctx, in.Input)
}
