// dummy.go defines a dummy node type for testing and placeholders.

package node

import (
	"github.com/xaionaro-go/avpipeline/processor"
)

type Dummy = Node[*processor.Dummy]
