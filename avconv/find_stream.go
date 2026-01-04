// find_stream.go provides a helper function to find a stream by its index in a format context.

package avconv

import (
	"context"

	"github.com/asticode/go-astiav"
)

func FindStreamByIndex(
	ctx context.Context,
	fmtCtx *astiav.FormatContext,
	streamIndex int,
) *astiav.Stream {
	for _, stream := range fmtCtx.Streams() {
		if stream.Index() == streamIndex {
			return stream
		}
	}
	return nil
}
