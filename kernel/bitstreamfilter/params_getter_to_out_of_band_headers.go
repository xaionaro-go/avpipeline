package bitstreamfilter

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
)

type ParamsGetterToOOBHeaders struct{}

var _ GetChainParamser = ParamsGetterToOOBHeaders{}

func (ParamsGetterToOOBHeaders) GetChainParams(
	ctx context.Context,
	input packet.Input,
) []Params {
	stream := input.GetStream()
	codecID := stream.CodecParameters().CodecID()
	params := ParamsMP2ToMP4(stream.CodecParameters().CodecID())
	logger.Debugf(ctx, "stream #%d: codec: %s: filters: %#+v", stream.Index(), codecID, params)
	return params
}

type ParamsGetterToCorrectedOOBHeaders struct{}

var _ GetChainParamser = ParamsGetterToCorrectedOOBHeaders{}

func (ParamsGetterToCorrectedOOBHeaders) GetChainParams(
	ctx context.Context,
	input packet.Input,
) []Params {
	stream := input.GetStream()
	codecID := stream.CodecParameters().CodecID()
	params := ParamsMP4ToMP4(stream.CodecParameters().CodecID())
	logger.Debugf(ctx, "stream #%d: codec: %s: filters: %#+v", stream.Index(), codecID, params)
	return params
}
