// static.go implements a condition that always returns a static boolean value.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
)

type Static bool

var _ Condition = (Static)(false)

func (v Static) String() string {
	return fmt.Sprintf("%t", v)
}

func (v Static) Match(context.Context, packet.Input) bool {
	return (bool)(v)
}
