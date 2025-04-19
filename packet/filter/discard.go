package filter

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

type AddPacketFlags struct {
	Condition condition.Condition
	Flag      astiav.PacketFlag
}

var _ condition.Condition = (*AddPacketFlags)(nil)

func NewAddPacketFlags(
	flag astiav.PacketFlag,
	cond condition.Condition,
) *AddPacketFlags {
	return &AddPacketFlags{
		Condition: cond,
		Flag:      flag,
	}
}

func (f *AddPacketFlags) String() string {
	return fmt.Sprintf("AddPacketFlags(%#+v, if:%s)", f.Flag, f.Condition)
}

func (f *AddPacketFlags) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	if !f.Condition.Match(ctx, pkt) {
		return true
	}
	flag := pkt.Flags()
	flag.Add(f.Flag)
	pkt.SetFlags(flag)
	return true
}
