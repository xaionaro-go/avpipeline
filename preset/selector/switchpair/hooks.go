package switchpair

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Hooks struct {
	OnSwitchRequest     func(ctx context.Context, to id.MemberID) error
	OnBeforeSwitch      func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID)
	OnInterruptedSwitch func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID)
	OnAfterSwitch       func(ctx context.Context, in packetorframe.InputUnion, from id.MemberID, to id.MemberID)
}

func (h Hooks) onSwitchRequest(
	ctx context.Context,
	_ packetorframe.InputUnion,
	to int32,
) error {
	if h.OnSwitchRequest == nil {
		return nil
	}
	return h.OnSwitchRequest(ctx, id.MemberID(to))
}

func (h Hooks) onBeforeSwitch(
	ctx context.Context,
	in packetorframe.InputUnion,
	from int32,
	to int32,
) {
	if h.OnBeforeSwitch == nil {
		return
	}
	h.OnBeforeSwitch(ctx, in, id.MemberID(from), id.MemberID(to))
}

func (h Hooks) onInterruptedSwitch(
	ctx context.Context,
	in packetorframe.InputUnion,
	from int32,
	to int32,
) {
	if h.OnInterruptedSwitch == nil {
		return
	}
	h.OnInterruptedSwitch(ctx, in, id.MemberID(from), id.MemberID(to))
}

func (h Hooks) onAfterSwitch(
	ctx context.Context,
	in packetorframe.InputUnion,
	from int32,
	to int32,
) {
	if h.OnAfterSwitch == nil {
		return
	}
	h.OnAfterSwitch(ctx, in, id.MemberID(from), id.MemberID(to))
}
