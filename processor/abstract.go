package processor

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
)

type Abstract interface {
	fmt.Stringer
	types.Closer

	SendInputPacketChan() chan<- packet.Input
	OutputPacketChan() <-chan packet.Output
	SendInputFrameChan() chan<- frame.Input
	OutputFrameChan() <-chan frame.Output
	ErrorChan() <-chan error
}
