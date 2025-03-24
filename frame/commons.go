package frame

import (
	"github.com/asticode/go-astiav"
)

type Commons struct {
	*astiav.FormatContext
	*astiav.Stream
	*astiav.Frame
	DTS int64
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
