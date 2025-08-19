package types

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type StateGetter interface {
	GetState(ctx context.Context, packet packetorframe.InputUnion) (State, <-chan struct{})
}
