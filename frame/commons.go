package frame

import (
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
)

type Commons struct {
	*astiav.FormatContext
	*astiav.Stream
	*astiav.Frame
	Pos      int64
	Duration int64
	DTS      int64
}

func (f *Commons) GetSize() int {
	return 0 // TODO: fix this
}

func (f *Commons) GetStreamIndex() int {
	return f.Stream.Index()
}

func (f *Commons) GetStream() *astiav.Stream {
	return f.Stream
}

func (f *Commons) GetFormatContext() *astiav.FormatContext {
	return f.FormatContext
}

func (f *Commons) GetDurationAsDuration() time.Duration {
	return avconv.Duration(f.Duration, f.Stream.TimeBase())
}

func (f *Commons) GetDTSAsDuration() time.Duration {
	return avconv.Duration(f.DTS, f.Stream.TimeBase())
}

func (f *Commons) GetPTSAsDuration() time.Duration {
	return avconv.Duration(f.Frame.Pts(), f.Stream.TimeBase())
}

func (f *Commons) GetStreamDurationAsDuration() time.Duration {
	return avconv.Duration(f.Stream.Duration(), f.Stream.TimeBase())
}
