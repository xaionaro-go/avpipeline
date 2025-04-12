package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/xsync"
)

type Until struct {
	xsync.Mutex
	Condition
}

var _ Condition = (*Until)(nil)

func NewUntil(cond Condition) *Until {
	return &Until{
		Condition: cond,
	}
}

func (v *Until) String() string {
	return fmt.Sprintf("Until(%s)", v.Condition)
}

func (v *Until) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return xsync.DoR1(ctx, &v.Mutex, func() bool {
		if v.Condition == nil {
			return false
		}
		if v.Condition.Match(ctx, pkt) {
			v.Condition = nil
			return false
		}

		return true
	})
}
