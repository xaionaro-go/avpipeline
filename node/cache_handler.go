package node

import (
	"context"

	"github.com/xaionaro-go/avpipeline/packet"
)

type CachingHandler interface {
	OnAddPushPacketsTo(
		ctx context.Context,
		pushTo PushPacketsTo,
	)
	OnRemovePushPacketsTo(
		ctx context.Context,
		pushTo PushPacketsTo,
	)
	RememberPacketIfNeeded(ctx context.Context, pkt packet.Input) error
	GetPendingPackets(
		ctx context.Context,
		pushTo PushPacketsTo,
	) ([]packet.Input, error)
	Reset(context.Context)
}
