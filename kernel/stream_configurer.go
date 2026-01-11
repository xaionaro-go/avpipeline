// stream_configurer.go defines the StreamConfigurer interface for configuring output streams.

package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type StreamConfigurer interface {
	fmt.Stringer
	StreamConfigure(
		ctx context.Context,
		outputStream *astiav.Stream,
		inputStreamIndex int,
	) error
}
