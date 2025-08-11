package autofix

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/types"
)

type streamIndexAssignerFLV struct{}

func (streamIndexAssignerFLV) StreamIndexAssign(
	ctx context.Context,
	input types.InputPacketOrFrameUnion,
) (_ret []int, _err error) {
	logger.Debugf(ctx, "StreamIndexAssign(): %d %s", input.GetStreamIndex(), input.GetMediaType())
	defer func() { logger.Debugf(ctx, "/StreamIndexAssign(): %v %v", _ret, _err) }()
	switch input.GetMediaType() {
	case astiav.MediaTypeVideo:
		return []int{0}, nil
	case astiav.MediaTypeAudio:
		return []int{1}, nil
	default:
		return nil, nil
	}
}
