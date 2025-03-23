package kernel

import (
	"context"

	"github.com/asticode/go-astiav"

	"github.com/xaionaro-go/avpipeline/stream"
)

type StreamConfigurerCopy struct{}

var _ StreamConfigurer = (*StreamConfigurerCopy)(nil)

func NewStreamConfigurerCopy() StreamConfigurerCopy {
	return StreamConfigurerCopy{}
}

func (sc *StreamConfigurerCopy) StreamConfigure(
	ctx context.Context,
	outputStream *astiav.Stream,
	inputStream *astiav.Stream,
) error {
	return stream.CopyParameters(
		ctx,
		outputStream,
		inputStream,
	)
}
