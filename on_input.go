package avpipeline

import (
	"github.com/xaionaro-go/avpipeline/types"
)

type OnInput interface {
	OnInput(types.InputPacket) bool
}
