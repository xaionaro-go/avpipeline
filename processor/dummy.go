package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
)

type Dummy struct {
	CountersStorage *Counters
}

var _ Abstract = (*Dummy)(nil)

func NewDummy() *Dummy {
	return &Dummy{
		CountersStorage: processortypes.NewCounters(),
	}
}

func (p *Dummy) String() string {
	return "Dummy"
}
func (p *Dummy) Close(ctx context.Context) error {
	return nil
}
func (p *Dummy) InputChan() chan<- packetorframe.InputUnion {
	return DiscardInputChan
}
func (p *Dummy) OutputChan() <-chan packetorframe.OutputUnion {
	return nil
}
func (p *Dummy) ErrorChan() <-chan error {
	return nil
}
func (p *Dummy) CountersPtr() *Counters {
	return p.CountersStorage
}
