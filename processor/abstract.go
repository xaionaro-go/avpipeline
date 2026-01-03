// Package processor provides the high-level, channel-based processing layer of the AV pipeline.
//
// abstract.go defines the Abstract interface, which is the core contract for all processors.
package processor

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
)

type Abstract interface {
	fmt.Stringer
	types.Closer

	InputChan() chan<- packetorframe.InputUnion
	OutputChan() <-chan packetorframe.OutputUnion
	ErrorChan() <-chan error

	CountersPtr() *Counters
}

/* for easier copy&paste:

func (p *MyFancyProcessorPlaceholder) String() string {

}
func (p *MyFancyProcessorPlaceholder) Close(ctx context.Context) error {

}
func (p *MyFancyProcessorPlaceholder) InputChan() chan<- packetorframe.InputUnion {

}
func (p *MyFancyProcessorPlaceholder) OutputChan() <-chan packetorframe.OutputUnion {

}
func (p *MyFancyProcessorPlaceholder) ErrorChan() <-chan error {

}
func (p *MyFancyProcessorPlaceholder) CountersPtr() *Counters {

}

*/
