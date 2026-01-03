// chan_struct.go defines ChanStruct, a helper struct that bundles input, output, and error channels.

package processor

import (
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type ChanStruct struct {
	InputCh  chan packetorframe.InputUnion
	OutputCh chan packetorframe.OutputUnion
	ErrorCh  chan error
}

func NewChanStruct(
	inputQueueSize uint,
	outputQueueSize uint,
	errorQueueSize uint,
) *ChanStruct {
	return &ChanStruct{
		InputCh:  make(chan packetorframe.InputUnion, inputQueueSize),
		OutputCh: make(chan packetorframe.OutputUnion, outputQueueSize),
		ErrorCh:  make(chan error, errorQueueSize),
	}
}
