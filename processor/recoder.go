package processor

import (
	"context"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
)

var DefaultOptionsRecoder = func() []Option {
	return []Option{
		OptionQueueSizeInputPacket(600),
		OptionQueueSizeInputFrame(600),
		OptionQueueSizeOutputPacket(10),
		OptionQueueSizeOutputFrame(10),
		OptionQueueSizeError(2),
	}
}

func NewRecoder(
	ctx context.Context,
	decoderFactory codec.DecoderFactory,
	encoderFactory codec.EncoderFactory,
	streamConfigurer kernel.StreamConfigurer,
	processorOpts ...Option,
) (_ret *FromKernel[*kernel.Recoder[codec.DecoderFactory, codec.EncoderFactory]], _err error) {
	logger.Debugf(ctx, "NewRecoder(ctx, %s, %s, %#+v, %#+v)", decoderFactory, encoderFactory, streamConfigurer, processorOpts)
	defer func() {
		logger.Debugf(ctx, "NewRecoder(ctx, %s, %s, %#+v, %#+v): %#+v, %v", decoderFactory, encoderFactory, streamConfigurer, processorOpts, _ret, _err)
	}()
	k, err := kernel.NewRecoder(
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
		append(DefaultOptionsRecoder(), processorOpts...)...,
	), nil
}
