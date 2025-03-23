package processor

import (
	"github.com/xaionaro-go/avpipeline/types"
)

type ChanStruct struct {
	InputCh  chan types.InputPacket
	OutputCh chan types.OutputPacket
	ErrorCh  chan error
}

func NewChanStruct(
	inputQueueSize uint,
	outputQueueSize uint,
	errorQueueSize uint,
) *ChanStruct {
	return &ChanStruct{
		InputCh:  make(chan types.InputPacket, inputQueueSize),
		OutputCh: make(chan types.OutputPacket, outputQueueSize),
		ErrorCh:  make(chan error, errorQueueSize),
	}
}
