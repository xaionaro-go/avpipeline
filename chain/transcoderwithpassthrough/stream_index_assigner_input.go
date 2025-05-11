package transcoderwithpassthrough

import (
	"context"

	"github.com/asticode/go-astiav"
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
	t *TranscoderWithPassthrough[C, P],
) *streamIndexAssignerInput[C, P] {
	s := &streamIndexAssignerInput[C, P]{
		StreamForward: t,
		AudioIndexMap: make(map[int]int),
		VideoIndexMap: make(map[int]int),
	}
	s.reload()
	return s
}

func (s *streamIndexAssignerInput[C, P]) Reload(ctx context.Context) {
	s.Locker.Do(ctx, s.reload)
}

func (s *streamIndexAssignerInput[C, P]) reload() {
	for k := range s.VideoIndexMap {
		delete(s.VideoIndexMap, k)
	}
	for k := range s.AudioIndexMap {
		delete(s.AudioIndexMap, k)
	}

	// TODO: we conflate two different ways of referencing a track, e.g.: global stream ID, and video track ID -- FIX IT.
	for outputVideoTrackID, cfg := range s.StreamForward.RecodingConfig.VideoTracks {
		for _, inputVideoTrackID := range cfg.InputTrackIDs {
			s.VideoIndexMap[inputVideoTrackID] = outputVideoTrackID
		}
	}

	for outputAudioTrackID, cfg := range s.StreamForward.RecodingConfig.AudioTracks {
		for _, inputAudioTrackID := range cfg.InputTrackIDs {
			s.AudioIndexMap[inputAudioTrackID] = outputAudioTrackID + len(s.VideoIndexMap)
		}
	}
}

func (s *streamIndexAssignerInput[C, P]) StreamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) (typing.Optional[int], error) {
	return xsync.DoA2R2(ctx, &s.Locker, s.streamIndexAssign, ctx, input)
}

func (s *streamIndexAssignerInput[C, P]) streamIndexAssign(
	ctx context.Context,
	input avptypes.InputPacketOrFrameUnion,
) (typing.Optional[int], error) {
	var v int
	var ok bool
	switch input.Frame.MediaType() {
	case astiav.MediaTypeVideo:
		v, ok = s.VideoIndexMap[input.Packet.GetStreamIndex()]
	case astiav.MediaTypeAudio:
		v, ok = s.AudioIndexMap[input.Packet.GetStreamIndex()]
	}
	if !ok {
		return typing.Optional[int]{}, nil
	}
	return typing.Opt(v), nil
}
