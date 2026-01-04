// stream_forwarder.go defines the StreamForwarder interface for forwarding streams between nodes.

package router

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
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
	transcoderConfig *transcodertypes.TranscoderConfig,
) (_ret StreamForwarder[CS, PS], _err error) {
	logger.Tracef(ctx, "NewStreamForwarder(ctx, %s, %s, %#+v)", src, dst, transcoderConfig)
	defer func() {
		logger.Tracef(ctx, "/NewStreamForwarder(ctx, %s, %s, %#+v): %v %v", src, dst, transcoderConfig, _ret, _err)
	}()
	var fwd StreamForwarder[CS, PS]
	var err error
	if transcoderConfig == nil {
		fwd, err = NewStreamForwarderCopy(ctx, src, dst)
	} else {
		fwd, err = NewStreamForwarderTranscoding(ctx, src, dst, transcoderConfig)
	}
	if err != nil {
		return nil, err
	}
	return fwd, nil
}
