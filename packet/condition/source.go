// source.go implements a condition that checks the source of a packet.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
)

type Source struct {
	packet.Source
}

func (c *Source) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return pkt.Source == c.Source
}

func (c *Source) String() string {
	return fmt.Sprintf("SourceIs(%v)", c.Source)
}
