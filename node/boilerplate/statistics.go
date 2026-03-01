// statistics.go provides boilerplate code for managing node statistics.

package boilerplate

import (
	"sync"

	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
)

type Counters struct {
	counters     *nodetypes.Counters
	countersOnce sync.Once
}

func (n *Counters) CountersPtr() *nodetypes.Counters {
	n.countersOnce.Do(func() {
		n.counters = nodetypes.NewCounters()
	})
	return n.counters
}
