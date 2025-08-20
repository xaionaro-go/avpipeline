package boilerplate

import (
	"github.com/xaionaro-go/avpipeline/node"
)

type Statistics struct {
	node.Statistics
}

func (n *Statistics) GetStatistics() *node.Statistics {
	return &n.Statistics
}
