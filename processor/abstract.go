package processor

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Abstract interface {
	fmt.Stringer
	types.Closer

	SendInputChan() chan<- types.InputPacket
	OutputPacketsChan() <-chan types.OutputPacket
	ErrorChan() <-chan error
}
