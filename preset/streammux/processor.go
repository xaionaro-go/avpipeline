package streammux

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ processor.Abstract = (*StreamMux[struct{}])(nil)

func (p *StreamMux[C]) SendInputPacketChan() chan<- packet.Input {
	return p.InputNode.Processor.InputPacketCh
}
func (p *StreamMux[C]) OutputPacketChan() <-chan packet.Output {
	return nil
}
func (p *StreamMux[C]) SendInputFrameChan() chan<- frame.Input {
	return p.InputNode.Processor.InputFrameCh
}
func (p *StreamMux[C]) OutputFrameChan() <-chan frame.Output {
	return nil
}
func (p *StreamMux[C]) ErrorChan() <-chan error {
	panic("not implemented")
}
