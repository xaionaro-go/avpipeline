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

	InputPacketChan() chan<- packet.Input
	OutputPacketChan() <-chan packet.Output
	InputFrameChan() chan<- frame.Input
	OutputFrameChan() <-chan frame.Output
	ErrorChan() <-chan error

	CountersPtr() *Counters
}

/* for easier copy&paste:

func (p *MyFancyProcessorPlaceholder) String() string {

}
func (p *MyFancyProcessorPlaceholder) Close(ctx context.Context) error {

}
func (p *MyFancyProcessorPlaceholder) InputPacketChan() chan<- packet.Input {

}
func (p *MyFancyProcessorPlaceholder) OutputPacketChan() <-chan packet.Output {

}
func (p *MyFancyProcessorPlaceholder) InputFrameChan() chan<- frame.Input {

}
func (p *MyFancyProcessorPlaceholder) OutputFrameChan() <-chan frame.Output {

}
func (p *MyFancyProcessorPlaceholder) ErrorChan() <-chan error {

}
func (p *MyFancyProcessorPlaceholder) CountersPtr() *Counters {

}

*/
