package boilerplate

import (
	"context"

	"github.com/go-ng/xatomic"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

type InputFilter struct {
	InputFilterCondition *packetorframefiltercondition.Condition
}

func (n *InputFilter) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {
	condPtr := xatomic.LoadPointer(&n.InputFilterCondition)
	if condPtr == nil {
		return nil
	}
	return *condPtr
}

func (n *InputFilter) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {
	xatomic.StorePointer(&n.InputFilterCondition, &cond)
}
