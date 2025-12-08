package node

import (
	"context"

	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
)

func AppendInputPacketFilter(ctx context.Context, n Abstract, cond packetfiltercondition.Condition) {
	f := n.GetInputPacketFilter(ctx)
	if f == nil {
		n.SetInputPacketFilter(ctx, cond)
		return
	}

	if f, ok := f.(packetfiltercondition.And); ok {
		n.SetInputPacketFilter(ctx, append(f, cond))
		return
	}

	n.SetInputPacketFilter(ctx, &packetfiltercondition.And{f, cond})
}

func AppendInputFrameFilter(ctx context.Context, n Abstract, cond framefiltercondition.Condition) {
	f := n.GetInputFrameFilter(ctx)
	if f == nil {
		n.SetInputFrameFilter(ctx, cond)
		return
	}

	if f, ok := f.(framefiltercondition.And); ok {
		n.SetInputFrameFilter(ctx, append(f, cond))
		return
	}

	n.SetInputFrameFilter(ctx, &framefiltercondition.And{f, cond})
}
