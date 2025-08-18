package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type Commons struct {
	*astiav.Frame
	CodecParameters *astiav.CodecParameters // TODO: remove this from here
	StreamIndex     int
	StreamsCount    int
	StreamDuration  int64
	TimeBase        astiav.Rational // TODO: reuse the time_base from the frame
	Pos             int64           // TODO: reuse pkt_pos from the frame
	Duration        int64           // TODO: reuse duration from the frame
}

func (f *Commons) GetMediaType() astiav.MediaType {
	return f.CodecParameters.MediaType()
}

func (f *Commons) GetTimeBase() astiav.Rational {
	return f.TimeBase
}

func (f *Commons) GetSize() int {
	return 0 // TODO: fix this
}

func (f *Commons) GetStreamIndex() int {
	return f.StreamIndex
}

func (f *Commons) GetDurationAsDuration() time.Duration {
	return avconv.Duration(f.Duration, f.TimeBase)
}

func (f *Commons) GetDTSAsDuration() time.Duration {
	return avconv.Duration(f.PktDts(), f.TimeBase)

}

func (f *Commons) GetPTS() int64 {
	return f.Frame.Pts()
}

func (f *Commons) GetDTS() int64 {
	return f.Frame.PktDts()
}

func (f *Commons) SetPTS(v int64) {
	f.Frame.SetPts(v)
}

func (f *Commons) SetDTS(v int64) {
	f.Frame.SetPktDts(v)
}

func (f *Commons) GetPTSAsDuration() time.Duration {
	return avconv.Duration(f.Frame.Pts(), f.TimeBase)
}

func (f *Commons) GetStreamDurationAsDuration() time.Duration {
	return avconv.Duration(f.StreamDuration, f.TimeBase)
}
