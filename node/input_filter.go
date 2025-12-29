package node

import (
	"context"

	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

func AppendInputFilter(ctx context.Context, n Abstract, cond packetorframefiltercondition.Condition) {
	f := n.GetInputFilter(ctx)
	if f == nil {
		n.SetInputFilter(ctx, cond)
		return
	}

	if f, ok := f.(packetorframefiltercondition.And); ok {
		n.SetInputFilter(ctx, append(f, cond))
		return
	}

	n.SetInputFilter(ctx, &packetorframefiltercondition.And{f, cond})
}
