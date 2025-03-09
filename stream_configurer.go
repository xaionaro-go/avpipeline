package avpipeline

import (
	"context"

	"github.com/asticode/go-astiav"
)

type StreamConfigurer interface {
	StreamConfigure(ctx context.Context, stream *astiav.Stream, pkt *astiav.Packet) error
}
