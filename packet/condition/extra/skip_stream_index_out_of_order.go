package extra

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

type SkipStreamIndexOutOfOrder struct {
	ExpectingStreamIndexGE int
	Locker                 xsync.Mutex
}

var _ condition.Condition = (*SkipStreamIndexOutOfOrder)(nil)

func NewSkipStreamIndexOutOfOrder() *SkipStreamIndexOutOfOrder {
	return &SkipStreamIndexOutOfOrder{}
}

func (i *SkipStreamIndexOutOfOrder) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	return xsync.DoR1(ctx, &i.Locker, func() bool {
		streamIndex := pkt.GetStreamIndex()
		logger.Tracef(ctx, "curIdx:%d; expectedNotLower:%d", i.ExpectingStreamIndexGE)
		switch {
		case streamIndex < i.ExpectingStreamIndexGE:
			return true
		case streamIndex == i.ExpectingStreamIndexGE:
			i.ExpectingStreamIndexGE++
			return true
		default:
			logger.Tracef(ctx, "skipping a package out of order, because stream index %d is higher than %d", streamIndex, i.ExpectingStreamIndexGE)
			return false
		}
	})
}

func (i *SkipStreamIndexOutOfOrder) String() string {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	idx := xsync.DoR1(ctx, &i.Locker, func() int {
		return i.ExpectingStreamIndexGE
	})
	return fmt.Sprintf("SkipStreamIndexOutOfOrder(accepting <= %d)", idx)
}
