// discard_chan.go provides a global channel that discards any input sent to it.

package processor

import (
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

var DiscardInputChan chan<- packetorframe.InputUnion = make(chan packetorframe.InputUnion)
