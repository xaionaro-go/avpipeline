package condition

import (
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[packet.Input]

/* for easier copy&paste:

func (c *) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {

}

func (c *) String() string {

}

*/
