package packetorframe

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
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
	GetTimeBase() astiav.Rational
	SetTimeBase(v astiav.Rational)
	GetDuration() int64
	SetDuration(v int64)
	GetPTS() int64
	GetDTS() int64
	SetPTS(v int64)
	SetDTS(v int64)
	GetPipelineSideData() types.PipelineSideData
	AddPipelineSideData(any) types.PipelineSideData
	IsKey() bool
	GetCodecParameters() *astiav.CodecParameters
	SetStreamIndex(int)
}

type StreamInfo struct {
	*astiav.Stream
	Source           AbstractSource
	PipelineSideData types.PipelineSideData

	// Fallback fields if Stream is nil
	CodecParameters *astiav.CodecParameters
	StreamIndex     int
	StreamsCount    int
	TimeBase        astiav.Rational
	Duration        int64
}

func (si *StreamInfo) GetStreamIndex() int {
	if si.Stream != nil {
		return si.Stream.Index()
	}
	return si.StreamIndex
}

func (si *StreamInfo) GetTimeBase() astiav.Rational {
	if si.Stream != nil {
		return si.Stream.TimeBase()
	}
	return si.TimeBase
}

func (si *StreamInfo) GetCodecParameters() *astiav.CodecParameters {
	if si.Stream != nil {
		return si.Stream.CodecParameters()
	}
	return si.CodecParameters
}

type InputUnion struct {
	Frame  *frame.Input
	Packet *packet.Input
}

type OutputUnion struct {
	Frame  *frame.Output
	Packet *packet.Output
}

var _ Abstract = (*InputUnion)(nil)
var _ Abstract = (*OutputUnion)(nil)

func (u *InputUnion) Get() Abstract {
	switch {
	case u.Frame != nil:
		return u.Frame
	case u.Packet != nil:
		return u.Packet
	default:
		return nil
	}
}

func (u *OutputUnion) Get() Abstract {
	switch {
	case u.Frame != nil:
		return u.Frame
	case u.Packet != nil:
		return u.Packet
	default:
		return nil
	}
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
func (u *InputUnion) GetTimeBase() astiav.Rational {
	return u.Get().GetTimeBase()
}
func (u *InputUnion) SetTimeBase(v astiav.Rational) {
	u.Get().SetTimeBase(v)
}
func (u *InputUnion) GetDuration() int64 {
	return u.Get().GetDuration()
}
func (u *InputUnion) SetDuration(v int64) {
	u.Get().SetDuration(v)
}
func (u *InputUnion) GetPipelineSideData() types.PipelineSideData {
	return u.Get().GetPipelineSideData()
}
func (u *InputUnion) AddPipelineSideData(obj any) types.PipelineSideData {
	return u.Get().AddPipelineSideData(obj)
}
func (u *InputUnion) IsKey() bool {
	return u.Get().IsKey()
}
func (u *InputUnion) GetCodecParameters() *astiav.CodecParameters {
	return u.Get().GetCodecParameters()
}
func (u *InputUnion) SetStreamIndex(v int) {
	u.Get().SetStreamIndex(v)
}

func (u *OutputUnion) GetSize() int {
	return u.Get().GetSize()
}
func (u *OutputUnion) GetStreamIndex() int {
	return u.Get().GetStreamIndex()
}
func (u *OutputUnion) SetStreamIndex(v int) {
	u.Get().SetStreamIndex(v)
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
func (u *OutputUnion) GetTimeBase() astiav.Rational {
	return u.Get().GetTimeBase()
}
func (u *OutputUnion) SetTimeBase(v astiav.Rational) {
	u.Get().SetTimeBase(v)
}
func (u *OutputUnion) GetDuration() int64 {
	return u.Get().GetDuration()
}
func (u *OutputUnion) SetDuration(v int64) {
	u.Get().SetDuration(v)
}
func (u *OutputUnion) GetPipelineSideData() types.PipelineSideData {
	return u.Get().GetPipelineSideData()
}
func (u *OutputUnion) AddPipelineSideData(obj any) types.PipelineSideData {
	return u.Get().AddPipelineSideData(obj)
}
func (u *OutputUnion) IsKey() bool {
	return u.Get().IsKey()
}
func (u *OutputUnion) GetCodecParameters() *astiav.CodecParameters {
	return u.Get().GetCodecParameters()
}

func (u *InputUnion) GetSource() AbstractSource {
	switch {
	case u.Packet != nil:
		return u.Packet.StreamInfo.Source
	case u.Frame != nil:
		return u.Frame.StreamInfo.Source
	default:
		return nil
	}
}

func (u *OutputUnion) ToInput() InputUnion {
	switch {
	case u.Packet != nil:
		return InputUnion{Packet: (*packet.Input)(u.Packet)}
	case u.Frame != nil:
		return InputUnion{Frame: (*frame.Input)(u.Frame)}
	default:
		return InputUnion{}
	}
}

func (u *InputUnion) Unwrap() (*packet.Input, *frame.Input) {
	return u.Packet, u.Frame
}

func (u *OutputUnion) Unwrap() (*packet.Output, *frame.Output) {
	return u.Packet, u.Frame
}

func (u *InputUnion) CloneAsReferencedOutput() OutputUnion {
	switch {
	case u.Packet != nil:
		var pkt *astiav.Packet
		if u.Packet.Packet != nil {
			pkt = packet.CloneAsReferenced(u.Packet.Packet)
		}
		return OutputUnion{
			Packet: ptr(packet.BuildOutput(
				pkt,
				u.Packet.StreamInfo,
			)),
		}
	case u.Frame != nil:
		var f *astiav.Frame
		if u.Frame.Frame != nil {
			f = frame.CloneAsReferenced(u.Frame.Frame)
		}
		return OutputUnion{
			Frame: ptr(frame.BuildOutput(
				f,
				u.Frame.StreamInfo,
			)),
		}
	default:
		return OutputUnion{}
	}
}

func (u *InputUnion) CloneAsReferencedInput() InputUnion {
	switch {
	case u.Packet != nil:
		var pkt *astiav.Packet
		if u.Packet.Packet != nil {
			pkt = packet.CloneAsReferenced(u.Packet.Packet)
		}
		return InputUnion{
			Packet: &packet.Input{
				Packet:     pkt,
				StreamInfo: u.Packet.StreamInfo,
			},
		}
	case u.Frame != nil:
		var f *astiav.Frame
		if u.Frame.Frame != nil {
			f = frame.CloneAsReferenced(u.Frame.Frame)
		}
		return InputUnion{
			Frame: &frame.Input{
				Frame:      f,
				StreamInfo: u.Frame.StreamInfo,
			},
		}
	default:
		return InputUnion{}
	}
}
