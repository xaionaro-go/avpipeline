// params_getter.go provides bitstream filter parameters and getter interface.

package bitstreamfilter

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
)

type Params struct {
	Name          Name
	SkipOnFailure bool
	Options       types.DictionaryItems
}

type GetChainParamser interface {
	fmt.Stringer

	// Note: "nil" skips initialization of bitstream filter for this stream on this packet,
	//       if you need to just not create filters for a stream at all, then return an empty slice instead.
	GetChainParams(context.Context, packet.Input) []Params
}
