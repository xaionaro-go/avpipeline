// frame_info.go defines the FrameInfo struct for storing metadata about media frames.

package kernel

import (
	"encoding/binary"
	"math"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packet"
)

type FrameInfo struct {
	// TODO: remove these: encoder should calculate timestamps from scratch, instead of reusing these
	PTS      int64
	DTS      int64
	TimeBase astiav.Rational
	Duration int64

	StreamIndex int
	FrameFlags  astiav.FrameFlags
	PictureType astiav.PictureType
}

func (p *FrameInfo) GetPictureType() astiav.PictureType {
	if p == nil {
		return math.MaxUint32
	}
	return p.PictureType
}

func (p *FrameInfo) Bytes() []byte {
	b := make([]byte, 64)
	binary.NativeEndian.PutUint64(b[0:8], uint64(p.PTS))
	binary.NativeEndian.PutUint64(b[8:16], uint64(p.DTS))
	binary.NativeEndian.PutUint64(b[16:24], uint64(p.TimeBase.Num()))
	binary.NativeEndian.PutUint64(b[24:32], uint64(p.TimeBase.Den()))
	binary.NativeEndian.PutUint64(b[32:40], uint64(p.Duration))
	binary.NativeEndian.PutUint64(b[40:48], uint64(p.StreamIndex))
	binary.NativeEndian.PutUint64(b[48:56], uint64(p.FrameFlags))
	binary.NativeEndian.PutUint64(b[56:64], uint64(p.PictureType))
	return b
}

func FrameInfoFromBytes(b []byte) *FrameInfo {
	if len(b) < 64 {
		return nil
	}
	num := int(binary.NativeEndian.Uint64(b[16:24]))
	den := int(binary.NativeEndian.Uint64(b[24:32]))
	return &FrameInfo{
		PTS:         int64(binary.NativeEndian.Uint64(b[0:8])),
		DTS:         int64(binary.NativeEndian.Uint64(b[8:16])),
		TimeBase:    astiav.NewRational(num, den),
		Duration:    int64(binary.NativeEndian.Uint64(b[32:40])),
		StreamIndex: int(binary.NativeEndian.Uint64(b[40:48])),
		FrameFlags:  astiav.FrameFlags(binary.NativeEndian.Uint64(b[48:56])),
		PictureType: astiav.PictureType(binary.NativeEndian.Uint64(b[56:64])),
	}
}

func FrameInfoFromPacketInput(pkt packet.Input) *FrameInfo {
	for _, sideData := range pkt.PipelineSideData {
		switch v := sideData.(type) {
		case *FrameInfo:
			return v
		}
	}
	return nil
}
