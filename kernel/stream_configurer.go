// stream_configurer.go defines the StreamConfigurer interface for configuring output streams.

package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
)

type StreamConfigurer interface {
	StreamConfigure(
		ctx context.Context,
		outputStream *astiav.Stream,
		inputStreamIndex int,
	) error
}
