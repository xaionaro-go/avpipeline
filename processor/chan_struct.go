package processor

type ChanStruct struct {
	InputCh  chan InputPacket
	OutputCh chan OutputPacket
	ErrorCh  chan error
}

func NewChanStruct(
	inputQueueSize uint,
	outputQueueSize uint,
	errorQueueSize uint,
) *ChanStruct {
	return &ChanStruct{
		InputCh:  make(chan InputPacket, inputQueueSize),
		OutputCh: make(chan OutputPacket, outputQueueSize),
		ErrorCh:  make(chan error, errorQueueSize),
	}
}
