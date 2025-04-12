package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type Commons struct {
	*astiav.Frame
	*astiav.CodecContext
	StreamIndex    int
	StreamsCount   int
	StreamDuration int64
	TimeBase       astiav.Rational
	Pos            int64
	Duration       int64
}

func (f *Commons) GetMediaType() astiav.MediaType {
	return f.CodecContext.MediaType()
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

func (f *Commons) GetCodecContext() *astiav.CodecContext {
	return f.CodecContext
}

func (f *Commons) GetDurationAsDuration() time.Duration {
	return avconv.Duration(f.Duration, f.CodecContext.TimeBase())
}

func (f *Commons) GetDTSAsDuration() time.Duration {
	return avconv.Duration(f.PktDts(), f.CodecContext.TimeBase())

}

func (f *Commons) GetPTS() int64 {
	return f.Frame.Pts()
}

func (f *Commons) GetPTSAsDuration() time.Duration {
	return avconv.Duration(f.Frame.Pts(), f.CodecContext.TimeBase())
}

func (f *Commons) GetStreamDurationAsDuration() time.Duration {
	return avconv.Duration(f.StreamDuration, f.CodecContext.TimeBase())
}
