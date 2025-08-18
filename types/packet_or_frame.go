package types

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type PacketOrFrame interface {
	frame.Input | packet.Input | frame.Output | packet.Output
}

type Frame interface {
	frame.Input | frame.Output
}

type Packet interface {
	packet.Input | packet.Output
}

type InputPacketOrFrame interface {
	frame.Input | packet.Input
}

type OutputPacketOrFrame interface {
	frame.Output | packet.Output
}

type PacketOrFramePointer[T PacketOrFrame] interface {
	*T
	AbstractPacketOrFrame
}

type AbstractPacketOrFrame interface {
	GetSize() int
	GetStreamIndex() int
	GetMediaType() astiav.MediaType
	GetPTS() int64
	GetDTS() int64
	SetPTS(v int64)
	SetDTS(v int64)
}

type InputPacketOrFrameUnion struct {
	Frame  *frame.Input
	Packet *packet.Input
}

var _ AbstractPacketOrFrame = (*InputPacketOrFrameUnion)(nil)

func (u *InputPacketOrFrameUnion) Get() AbstractPacketOrFrame {
	if u.Frame != nil {
		return u.Frame
	}
	if u.Packet != nil {
		return u.Packet
	}
	return nil
}
func (u *InputPacketOrFrameUnion) GetSize() int {
	return u.Get().GetSize()
}
func (u *InputPacketOrFrameUnion) GetStreamIndex() int {
	return u.Get().GetStreamIndex()
}
func (u *InputPacketOrFrameUnion) GetMediaType() astiav.MediaType {
	return u.Get().GetMediaType()
}
func (u *InputPacketOrFrameUnion) GetPTS() int64 {
	return u.Get().GetPTS()
}
func (u *InputPacketOrFrameUnion) GetDTS() int64 {
	return u.Get().GetDTS()
}
func (u *InputPacketOrFrameUnion) SetPTS(v int64) {
	u.Get().SetPTS(v)
}
func (u *InputPacketOrFrameUnion) SetDTS(v int64) {
	u.Get().SetDTS(v)
}
