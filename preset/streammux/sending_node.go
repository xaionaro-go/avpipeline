package streammux

import (
	"context"

	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type SendingNode interface {
	node.Abstract
	SetCustomData(v OutputCustomData)
	GetCustomData() OutputCustomData
}

type SetDropOnCloser interface {
	SetDropOnClose(ctx context.Context, v bool) error
}

type SenderFactory interface {
	NewSender(
		ctx context.Context,
		senderKey SenderKey,
	) (SendingNode, types.SenderConfig, error)
}

type ErrNoSetDropOnClose struct{}

func (e ErrNoSetDropOnClose) Error() string {
	return "sending node does not implement SetDropOnCloser"
}

func sendingNodeSetDropOnClose(
	ctx context.Context,
	sendingNode SendingNode,
	v bool,
) error {
	s, ok := sendingNode.(SetDropOnCloser)
	if !ok {
		return ErrNoSetDropOnClose{}
	}
	return s.SetDropOnClose(ctx, true)
}
