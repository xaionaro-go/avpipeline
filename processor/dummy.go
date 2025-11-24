package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
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
func (p *Dummy) InputPacketChan() chan<- packet.Input {
	return DiscardInputPacketChan
}
func (p *Dummy) OutputPacketChan() <-chan packet.Output {
	return nil
}
func (p *Dummy) InputFrameChan() chan<- frame.Input {
	return DiscardInputFrameChan
}
func (p *Dummy) OutputFrameChan() <-chan frame.Output {
	return nil
}
func (p *Dummy) ErrorChan() <-chan error {
	return nil
}
func (p *Dummy) CountersPtr() *Counters {
	return p.CountersStorage
}
