package addpacketflags

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

type Filter struct {
	Condition condition.Condition
	Flag      astiav.PacketFlag
}

var _ condition.Condition = (*Filter)(nil)

func New(
	flag astiav.PacketFlag,
	cond condition.Condition,
) *Filter {
	return &Filter{
		Condition: cond,
		Flag:      flag,
	}
}

func (f *Filter) String() string {
	return fmt.Sprintf("AddPacketFlags(%#+v, if:%s)", f.Flag, f.Condition)
}

func (f *Filter) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	if f.Condition != nil && !f.Condition.Match(ctx, pkt) {
		return true
	}
	flag := pkt.Flags()
	pkt.SetFlags(flag.Add(f.Flag))
	return true
}
