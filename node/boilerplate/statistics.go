package boilerplate

import (
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
)

type Counters struct {
	*nodetypes.Counters
}

func (n *Counters) CountersPtr() *nodetypes.Counters {
	return nodetypes.NewCounters()
}
