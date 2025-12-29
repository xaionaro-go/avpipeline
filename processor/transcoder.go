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
	streamConfigurer kernel.StreamConfigurer,
	processorOpts ...Option,
) (_ret *FromKernel[*kernel.Transcoder[codec.DecoderFactory, codec.EncoderFactory]], _err error) {
	logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %#+v, %#+v)", decoderFactory, encoderFactory, streamConfigurer, processorOpts)
	defer func() {
		logger.Debugf(ctx, "NewTranscoder(ctx, %s, %s, %#+v, %#+v): %#+v, %v", decoderFactory, encoderFactory, streamConfigurer, processorOpts, _ret, _err)
	}()
	k, err := kernel.NewTranscoder(
		ctx,
		decoderFactory,
		encoderFactory,
		streamConfigurer,
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
