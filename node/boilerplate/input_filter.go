package boilerplate

import (
	"github.com/go-ng/xatomic"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

type InputFilter struct {
	PacketFilterCondition *packetfiltercondition.Condition
	FrameFilterCondition  *framefiltercondition.Condition
}

func (n *InputFilter) GetInputPacketFilter() packetfiltercondition.Condition {
	condPtr := xatomic.LoadPointer(&n.PacketFilterCondition)
	if condPtr == nil {
		return nil
	}
	return *condPtr
}

func (n *InputFilter) SetInputPacketFilter(
	cond packetfiltercondition.Condition,
) {
	xatomic.StorePointer(&n.PacketFilterCondition, &cond)
}

func (n *InputFilter) GetInputFrameFilter() framefiltercondition.Condition {
	condPtr := xatomic.LoadPointer(&n.FrameFilterCondition)
	if condPtr == nil {
		return nil
	}
	return *condPtr
}

func (n *InputFilter) SetInputFrameFilter(
	cond framefiltercondition.Condition,
) {
	xatomic.StorePointer(&n.FrameFilterCondition, &cond)
}
