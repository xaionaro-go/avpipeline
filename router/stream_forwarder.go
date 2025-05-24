package router

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/node"
	transcodertypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
	"github.com/xaionaro-go/avpipeline/processor"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarder[CS any, PS processor.Abstract] interface {
	Start(context.Context) error
	Stop(context.Context) error
	Source() *node.NodeWithCustomData[CS, PS]
	Destination() node.Abstract
}

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarder[CS any, PS processor.Abstract](
	ctx context.Context,
	src *node.NodeWithCustomData[CS, PS],
	dst node.Abstract,
	recoderConfig *transcodertypes.RecoderConfig,
) (_ret StreamForwarder[CS, PS], _err error) {
	logger.Tracef(ctx, "NewForwarder(ctx, %s, %s, %#+v)", src, dst, recoderConfig)
	defer func() {
		logger.Tracef(ctx, "/NewForwarder(ctx, %s, %s, %#+v): %v %v", src, dst, recoderConfig, _ret, _err)
	}()
	var fwd StreamForwarder[CS, PS]
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
