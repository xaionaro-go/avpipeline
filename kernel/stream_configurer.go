package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
)

type StreamConfigurer interface {
	StreamConfigure(
		ctx context.Context,
		outputStream *astiav.Stream,
		inputStream *astiav.Stream,
	) error
}
