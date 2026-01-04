// condition.go defines the Condition interface for filtering media packets.

// Package condition provides various conditions for filtering media packets.
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
