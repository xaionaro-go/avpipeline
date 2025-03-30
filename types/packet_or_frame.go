package types

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type InputPacketOrFrame struct {
	Packet *packet.Input
	Frame  *frame.Input
}

func (i *InputPacketOrFrame) GetPTS() int64 {
	if i.Frame != nil {
		return i.Frame.Pts()
	}
	return i.Packet.Pts()
}

func (i *InputPacketOrFrame) GetStream() *astiav.Stream {
	if i.Frame != nil {
		return i.Frame.Stream
	}
	return i.Packet.Stream
}
