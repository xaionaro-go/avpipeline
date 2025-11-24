package processor

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

var (
	DiscardInputFrameChan  chan<- frame.Input  = make(chan frame.Input)
	DiscardInputPacketChan chan<- packet.Input = make(chan packet.Input)
)
