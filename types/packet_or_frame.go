package types

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type InputPacketOrFrame struct {
	Packet *packet.Input
	Frame  *frame.Input
}

func (i *InputPacketOrFrame) GetStreamIndex() int {
	if i.Frame != nil {
		return i.Frame.GetStreamIndex()
	}
	return i.Packet.GetStreamIndex()
}

func (i *InputPacketOrFrame) GetPTS() int64 {
	if i.Frame != nil {
		return i.Frame.Pts()
	}
	return i.Packet.Pts()
}
