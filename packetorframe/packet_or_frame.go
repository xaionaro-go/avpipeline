package packetorframe

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Any interface {
	Input | Output
}

type Input interface {
	frame.Input | packet.Input
}

type Output interface {
	frame.Output | packet.Output
}

type Pointer[T Any] interface {
	*T
	Abstract
}

type Abstract interface {
	GetSize() int
	GetStreamIndex() int
	GetMediaType() astiav.MediaType
	GetPTS() int64
	GetDTS() int64
	SetPTS(v int64)
	SetDTS(v int64)
}

type InputUnion struct {
	Frame  *frame.Input
	Packet *packet.Input
}

var _ Abstract = (*InputUnion)(nil)

func (u *InputUnion) Get() Abstract {
	if u.Frame != nil {
		return u.Frame
	}
	if u.Packet != nil {
		return u.Packet
	}
	return nil
}
func (u *InputUnion) GetSize() int {
	return u.Get().GetSize()
}
func (u *InputUnion) GetStreamIndex() int {
	return u.Get().GetStreamIndex()
}
func (u *InputUnion) GetMediaType() astiav.MediaType {
	return u.Get().GetMediaType()
}
func (u *InputUnion) GetPTS() int64 {
	return u.Get().GetPTS()
}
func (u *InputUnion) GetDTS() int64 {
	return u.Get().GetDTS()
}
func (u *InputUnion) SetPTS(v int64) {
	u.Get().SetPTS(v)
}
func (u *InputUnion) SetDTS(v int64) {
	u.Get().SetDTS(v)
}

type OutputUnion struct {
	Frame  *frame.Output
	Packet *packet.Output
}

var _ Abstract = (*OutputUnion)(nil)

func (u *OutputUnion) Get() Abstract {
	if u.Frame != nil {
		return u.Frame
	}
	if u.Packet != nil {
		return u.Packet
	}
	return nil
}
func (u *OutputUnion) GetSize() int {
	return u.Get().GetSize()
}
func (u *OutputUnion) GetStreamIndex() int {
	return u.Get().GetStreamIndex()
}
func (u *OutputUnion) GetMediaType() astiav.MediaType {
	return u.Get().GetMediaType()
}
func (u *OutputUnion) GetPTS() int64 {
	return u.Get().GetPTS()
}
func (u *OutputUnion) GetDTS() int64 {
	return u.Get().GetDTS()
}
func (u *OutputUnion) SetPTS(v int64) {
	u.Get().SetPTS(v)
}
func (u *OutputUnion) SetDTS(v int64) {
	u.Get().SetDTS(v)
}
