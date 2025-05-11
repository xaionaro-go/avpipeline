package router

import (
	"context"

	transcodertypes "github.com/xaionaro-go/avpipeline/chain/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/node"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarder interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Source() *NodeRouting
	Destination() node.Abstract
}

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarder(
	ctx context.Context,
	src *NodeRouting,
	dst node.Abstract,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret StreamForwarder, _err error) {
	var fwd StreamForwarder
	var err error
	if recoderConfig == nil {
		fwd, err = NewStreamForwarderCopy(ctx, src, dst)
	} else {
		fwd, err = NewStreamForwarderRecoding(ctx, src, dst, recoderConfig)
	}
	if err != nil {
		return nil, err
	}
	return fwd, nil
}
