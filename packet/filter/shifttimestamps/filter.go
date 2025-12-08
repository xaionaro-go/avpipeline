package shifttimestamps

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

type Filter struct {
	Condition condition.Condition
	Offset    int64
}

var _ condition.Condition = (*Filter)(nil)

func New(
	offset int64,
	cond condition.Condition,
) *Filter {
	return &Filter{
		Condition: cond,
		Offset:    offset,
	}
}

func (f *Filter) String() string {
	return fmt.Sprintf("ShiftTimestamps(%#+v, if:%s)", f.Offset, f.Condition)
}

func (f *Filter) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	if f.Condition != nil && !f.Condition.Match(ctx, pkt) {
		return true
	}
	dts, pts := pkt.Dts(), pkt.Pts()
	isNoDTS := dts == consts.NoPTSValue
	isNoPTS := pts == consts.NoPTSValue
	if !isNoDTS {
		pkt.SetDts(dts + f.Offset)
	}
	if !isNoPTS {
		pkt.SetPts(pts + f.Offset)
	}
	return true
}
