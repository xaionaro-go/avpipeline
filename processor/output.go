// output.go provides factory functions to instantiate FromKernel processors for output destinations.

package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/secret"
)

// DefaultOptionsOutput returns the default Option set applied by
// NewOutputFromURL when constructing the underlying FromKernel processor
// for an output (muxer) node.
//
// The returned options bound the per-edge in-flight backlog feeding the
// output kernel:
//   - Input queue: 60 frames. At 1080p YUV420 (~3.1 MB per AVFrame) this
//     caps the worst-case input-side backlog at roughly 186 MB per edge.
//     At 60 fps this corresponds to ~1 s of buffering; at 30 fps, ~2 s.
//   - Output queue: 0 (the output node is a graph sink — it muxes/sends
//     downstream and does not push frames to further pipeline edges).
//   - Error queue: 2 entries (errors are rare and consumed promptly).
//
// The historical default for the input queue was 600 frames, which on the
// output side directly feeds the muxer; a slow remote sink combined with
// a 600-cap input edge could buffer ~1.86 GB of AVFrames at 1080p YUV420
// per output node before backpressure was meaningful. The default was
// lowered to 60 frames in commit 44bdfbc to bound this worst case ~10x
// lower; per-edge frame_drop barriers cover transient stalls beyond this
// window.
//
// Override paths:
//   - SetDefaultQueueSizes: override the package-level defaults at process
//     startup with input validation. Preferred over mutating package state
//     directly.
//   - Per-call overrides: pass OptionQueueSize{Input,Output,Error} entries
//     via processorOpts to NewOutputFromURL (later options win).
func DefaultOptionsOutput() []Option {
	sizes := loadDefaultQueueSizes().Output
	return []Option{
		OptionQueueSizeInput(sizes.Input),
		OptionQueueSizeOutput(sizes.Output),
		OptionQueueSizeError(sizes.Error),
	}
}

func NewOutputFromURL(
	ctx context.Context,
	urlString string,
	streamKey secret.String,
	cfg kernel.OutputConfig,
	processorOpts ...Option,
) (*FromKernel[*kernel.Output], error) {
	k, err := kernel.NewOutputFromURL(ctx, urlString, streamKey, cfg)
	if err != nil {
		return nil, err
	}
	return NewFromKernel(
		ctx,
		k,
		append(DefaultOptionsOutput(), processorOpts...)...,
	), nil
}
