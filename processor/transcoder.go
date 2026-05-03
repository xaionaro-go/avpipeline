// transcoder.go provides factory functions to instantiate FromKernel processors for transcoding.

package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
)

// DefaultOptionsTranscoder returns the default Option set applied by
// NewTranscoder when constructing the underlying FromKernel processor.
//
// The returned options bound the per-edge in-flight backlog around the
// transcoder node:
//   - Input queue: 60 frames. At 1080p YUV420 (~3.1 MB per AVFrame) this
//     caps the worst-case input-side backlog at roughly 186 MB per edge.
//     At 60 fps this corresponds to ~1 s of buffering; at 30 fps, ~2 s.
//   - Output queue: 10 frames (kept small; downstream nodes typically
//     impose their own buffering).
//   - Error queue: 2 entries (errors are rare and consumed promptly).
//
// The historical default for the input queue was 600 frames, which under
// sustained 60 fps load and a slow drain event could accumulate multi-GB
// AVFrame backlogs across a multi-edge production graph (~1.86 GB per
// 600-cap edge at 1080p YUV420). The default was lowered to 60 frames in
// commit 44bdfbc to bound this worst case ~10x lower; per-edge frame_drop
// barriers cover transient stalls beyond this window.
//
// Override paths:
//   - SetDefaultQueueSizes: override the package-level defaults at process
//     startup with input validation. Preferred over mutating package state
//     directly.
//   - Per-call overrides: pass OptionQueueSize{Input,Output,Error} entries
//     via processorOpts to NewTranscoder (later options win).
func DefaultOptionsTranscoder() []Option {
	sizes := loadDefaultQueueSizes().Transcoder
	return []Option{
		OptionQueueSizeInput(sizes.Input),
		OptionQueueSizeOutput(sizes.Output),
		OptionQueueSizeError(sizes.Error),
	}
}

func NewTranscoder(
	ctx context.Context,
	decoderFactory codec.DecoderFactory,
	encoderFactory codec.EncoderFactory,
	encoderConfig *kernel.EncoderConfig,
	processorOpts ...Option,
) (_ret *FromKernel[*kernel.Transcoder[codec.DecoderFactory, codec.EncoderFactory]], _err error) {
	logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %#+v, %#+v)", decoderFactory, encoderFactory, encoderConfig, processorOpts)
	defer func() {
		logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %#+v, %#+v): %#+v, %v", decoderFactory, encoderFactory, encoderConfig, processorOpts, _ret, _err)
	}()
	k, err := kernel.NewTranscoder(
		ctx,
		decoderFactory,
		encoderFactory,
		encoderConfig,
	)
	if err != nil {
		return nil, err
	}
	return NewFromKernel(
		ctx,
		k,
		append(DefaultOptionsTranscoder(), processorOpts...)...,
	), nil
}
