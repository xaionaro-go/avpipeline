package processor

import (
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

var (
	DiscardInputChan chan<- packetorframe.InputUnion = make(chan packetorframe.InputUnion)
)
