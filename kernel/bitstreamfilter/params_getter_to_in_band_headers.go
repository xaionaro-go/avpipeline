package bitstreamfilter

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
)

type ParamsGetterToInBandHeaders struct{}

var _ GetChainParamser = ParamsGetterToInBandHeaders{}

func (ParamsGetterToInBandHeaders) GetChainParams(
	ctx context.Context,
	input packet.Input,
) []Params {
	stream := input.GetStream()
	codecID := stream.CodecParameters().CodecID()
	params := ParamsMP4ToMP2(stream.CodecParameters().CodecID())
	logger.Debugf(ctx, "stream #%d: codec: %s: filters: %#+v", stream.Index(), codecID, params)
	return params
}

func (ParamsGetterToInBandHeaders) String() string {
	return "ToInBandHeaders"
}
