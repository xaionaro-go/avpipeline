package processor

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type ChanStruct struct {
	InputPacketCh  chan packet.Input
	OutputPacketCh chan packet.Output
	InputFrameCh   chan frame.Input
	OutputFrameCh  chan frame.Output
	ErrorCh        chan error
}

func NewChanStruct(
	inputPacketQueueSize uint,
	outputPacketQueueSize uint,
	inputFrameQueueSize uint,
	outputFrameQueueSize uint,
	errorQueueSize uint,
) *ChanStruct {
	return &ChanStruct{
		InputPacketCh:  make(chan packet.Input, inputPacketQueueSize),
		OutputPacketCh: make(chan packet.Output, outputPacketQueueSize),
		InputFrameCh:   make(chan frame.Input, inputFrameQueueSize),
		OutputFrameCh:  make(chan frame.Output, outputFrameQueueSize),
		ErrorCh:        make(chan error, errorQueueSize),
	}
}
