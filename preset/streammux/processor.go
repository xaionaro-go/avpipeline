package streammux

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
)

var _ processor.Abstract = (*StreamMux[struct{}])(nil)

func (s *StreamMux[C]) InputPacketChan() chan<- packet.Input {
	return s.InputNode.Processor.InputPacketCh
}
func (s *StreamMux[C]) OutputPacketChan() <-chan packet.Output {
	return nil
}
func (s *StreamMux[C]) InputFrameChan() chan<- frame.Input {
	return s.InputNode.Processor.InputFrameCh
}
func (s *StreamMux[C]) OutputFrameChan() <-chan frame.Output {
	return nil
}
func (s *StreamMux[C]) ErrorChan() <-chan error {
	panic("not implemented")
}
