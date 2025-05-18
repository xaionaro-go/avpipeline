package autofix

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
)

type streamIndexAssignerFLV struct{}

func (streamIndexAssignerFLV) StreamIndexAssign(
	ctx context.Context,
	input types.InputPacketOrFrameUnion,
) (_ret typing.Optional[int], _err error) {
	logger.Debugf(ctx, "StreamIndexAssign(): %d %s", input.GetStreamIndex(), input.GetMediaType())
	defer func() { logger.Debugf(ctx, "/StreamIndexAssign(): %t:%v %v", _ret.IsSet(), _ret.GetOrZero(), _err) }()
	switch input.GetMediaType() {
	case astiav.MediaTypeVideo:
		return typing.Opt(0), nil
	case astiav.MediaTypeAudio:
		return typing.Opt(1), nil
	default:
		return typing.Optional[int]{}, nil
	}
}
