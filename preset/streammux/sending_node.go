package streammux

import (
	"context"

	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type SendingNode[C any] interface {
	node.Abstract
	SetCustomData(v OutputCustomData[C])
	GetCustomData() OutputCustomData[C]
}

type SetDropOnCloser interface {
	SetDropOnClose(ctx context.Context, v bool) error
}

type SenderFactory[C any] interface {
	NewSender(
		ctx context.Context,
		senderKey SenderKey,
	) (SendingNode[C], types.SenderConfig, error)
}

type ErrNoSetDropOnClose struct{}

func (e ErrNoSetDropOnClose) Error() string {
	return "sending node does not implement SetDropOnCloser"
}

func sendingNodeSetDropOnClose[C any](
	ctx context.Context,
	sendingNode SendingNode[C],
	v bool,
) error {
	s, ok := sendingNode.(SetDropOnCloser)
	if !ok {
		return ErrNoSetDropOnClose{}
	}
	return s.SetDropOnClose(ctx, v)
}
