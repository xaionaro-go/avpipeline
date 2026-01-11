// transcoder.go provides factory functions to instantiate FromKernel processors for transcoding.

package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
)

var DefaultOptionsTranscoder = func() []Option {
	return []Option{
		OptionQueueSizeInput(600),
		OptionQueueSizeOutput(10),
		OptionQueueSizeError(2),
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
