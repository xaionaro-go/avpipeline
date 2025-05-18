package bitstreamfilter

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
)

type Params struct {
	Name Name
}

type GetChainParamser interface {
	// Note: "nil" skips initialization of bitstream filter for this stream on this packet,
	//       if you need to just not create filters for a stream at all, then return an empty slice instead.
	GetChainParams(context.Context, packet.Input) []Params
}
