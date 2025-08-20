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

/* for easier copy&paste:

func (p *MyFancyProcessorPlaceholder) Close(ctx context.Context) error {

}
func (p *MyFancyProcessorPlaceholder) SendInputPacketChan() chan<- packet.Input {

}
func (p *MyFancyProcessorPlaceholder) OutputPacketChan() <-chan packet.Output {

}
func (p *MyFancyProcessorPlaceholder) SendInputFrameChan() chan<- frame.Input {

}
func (p *MyFancyProcessorPlaceholder) OutputFrameChan() <-chan frame.Output {

}
func (p *MyFancyProcessorPlaceholder) ErrorChan() <-chan error {

}

*/
