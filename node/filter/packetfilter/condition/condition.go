package condition

import (
	"github.com/xaionaro-go/avpipeline/node/filter"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Input = filter.Input[packet.Input]
type Condition = filter.Condition[packet.Input]

/* for easier copy&paste:

import (
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

type PacketFilter struct{

}

var _ packetfiltercondition.Condition = (*PacketFilter)(nil)

func New() *PacketFilter {
	return &PacketFilter{}
}

func (f *PacketFilter) String() string {
	return
}

func (f *PacketFilter) Match(
	ctx context.Context,
	in packetfiltercondition.Input,
) bool {

}


*/
