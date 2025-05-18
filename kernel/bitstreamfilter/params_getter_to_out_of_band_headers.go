package bitstreamfilter

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
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
	filterName := NameMP2ToMP4(stream.CodecParameters().CodecID())
	logger.Debugf(ctx, "stream #%d: codec: %s: filter: %s", stream.Index(), codecID, filterName)
	if filterName == NameNull {
		return []Params{}
	}
	return []Params{{Name: filterName}}
}
