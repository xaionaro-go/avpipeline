// cache_handler.go defines the CachingHandler interface for nodes.

package node

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
)

type CachingHandler interface {
	OnAddPushTo(
		ctx context.Context,
		pushTo PushTo,
	)
	OnRemovePushTo(
		ctx context.Context,
		pushTo PushTo,
	)
	RememberPacketIfNeeded(ctx context.Context, pkt packet.Input) error
	GetPending(
		ctx context.Context,
		pushTo PushTo,
	) ([]packet.Input, error)
	Reset(context.Context)
}
