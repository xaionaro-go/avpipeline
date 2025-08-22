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
	if stream == nil {
		logger.Errorf(ctx, "no stream associated with the input packet")
		return nil
	}
	codecParams := stream.CodecParameters()
	if codecParams == nil {
		logger.Errorf(ctx, "no codec parameters associated with the input packet's stream")
		return nil
	}
	codecID := codecParams.CodecID()
	params := ParamsMP4ToMP2(codecParams.CodecID())
	logger.Debugf(ctx, "stream #%d: codec: %s: filters: %#+v", stream.Index(), codecID, params)
	return params
}

func (ParamsGetterToInBandHeaders) String() string {
	return "ToInBandHeaders"
}
