package transcoderwithpassthrough

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/processor"
	avptypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/typing"
	"github.com/xaionaro-go/xsync"
)

type streamIndexAssignerInput[C any, P processor.Abstract] struct {
	StreamForward *TranscoderWithPassthrough[C, P]
	AudioIndexMap map[int]int
	VideoIndexMap map[int]int
	Locker        xsync.Mutex
}

func newStreamIndexAssignerInput[C any, P processor.Abstract](
	ctx context.Context,
	t *TranscoderWithPassthrough[C, P],
) *streamIndexAssignerInput[C, P] {
	s := &streamIndexAssignerInput[C, P]{
		StreamForward: t,
		AudioIndexMap: make(map[int]int),
		VideoIndexMap: make(map[int]int),
	}
	s.reloadLocked(ctx)
	return s
}

func (s *streamIndexAssignerInput[C, P]) Reload(ctx context.Context) {
	s.StreamForward.locker.Do(ctx, func() {
		s.reload(ctx)
	})
}

func (s *streamIndexAssignerInput[C, P]) reload(
	ctx context.Context,
) {
	s.Locker.Do(ctx, func() {
		s.reloadLocked(ctx)
	})
}

func (s *streamIndexAssignerInput[C, P]) reloadLocked(
	ctx context.Context,
) {
	if logger.FromCtx(ctx).Level() >= logger.LevelDebug {
		logger.Debugf(ctx, "%#+v", spew.Sdump(s.StreamForward.RecodingConfig))
	}

	for k := range s.VideoIndexMap {
		delete(s.VideoIndexMap, k)
	}
	for k := range s.AudioIndexMap {
		delete(s.AudioIndexMap, k)
	}

	// TODO: we conflate two different ways of referencing a track, e.g.: global stream ID, and video track ID -- FIX IT.
	maxVideoTrackID := 0
	for outputVideoTrackID, cfg := range s.StreamForward.RecodingConfig.VideoTracks {
		for _, inputVideoTrackID := range cfg.InputTrackIDs {
			s.VideoIndexMap[inputVideoTrackID] = outputVideoTrackID
			if outputVideoTrackID > maxVideoTrackID {
				maxVideoTrackID = outputVideoTrackID
			}
		}
	}

	for outputAudioTrackID, cfg := range s.StreamForward.RecodingConfig.AudioTracks {
		for _, inputAudioTrackID := range cfg.InputTrackIDs {
			s.AudioIndexMap[inputAudioTrackID] = outputAudioTrackID + maxVideoTrackID + 1
		}
	}

	if logger.FromCtx(ctx).Level() >= logger.LevelDebug {
		logger.Debugf(ctx, "video: %s; audio: %s", spew.Sdump(s.VideoIndexMap), spew.Sdump(s.AudioIndexMap))
	}
}

func (s *streamIndexAssignerInput[C, P]) StreamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) (_ret typing.Optional[int], _err error) {
	defer func() { logger.Tracef(ctx, "StreamIndexAssign: %v, %v", _ret, _err) }()
	return xsync.DoA2R2(ctx, &s.Locker, s.streamIndexAssign, ctx, input)
}

func (s *streamIndexAssignerInput[C, P]) streamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) (typing.Optional[int], error) {
	if len(s.VideoIndexMap) == 0 && len(s.AudioIndexMap) == 0 {
		return typing.Opt(input.GetStreamIndex()), nil
	}

	var v int
	var ok bool
	switch input.GetMediaType() {
	case astiav.MediaTypeVideo:
		v, ok = s.VideoIndexMap[input.GetStreamIndex()]
	case astiav.MediaTypeAudio:
		v, ok = s.AudioIndexMap[input.GetStreamIndex()]
	}
	if !ok {
		return typing.Optional[int]{}, nil
	}
	return typing.Opt(v), nil
}
