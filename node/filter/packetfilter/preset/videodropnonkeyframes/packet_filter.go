package videodropnonkeyframes

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/xsync"
)

type PacketFilter struct {
	Locker              xsync.Mutex
	Condition           packetfiltercondition.Condition
	WaitingForKeyFrame  map[int]struct{}
	TotalDroppedPackets uint64
	TotalDroppedBytes   uint64
}

var _ packetfiltercondition.Condition = (*PacketFilter)(nil)

func New(cond packetfiltercondition.Condition) *PacketFilter {
	return &PacketFilter{
		Condition:          cond,
		WaitingForKeyFrame: make(map[int]struct{}),
	}
}

func (f *PacketFilter) String() string {
	return fmt.Sprintf("DropNonKeyFrames(%s)", f.Condition)
}

func (f *PacketFilter) Match(
	ctx context.Context,
	in packetfiltercondition.Input,
) bool {
	if in.Input.GetMediaType() != astiav.MediaTypeVideo {
		return true
	}
	return xsync.DoA2R1(ctx, &f.Locker, f.match, ctx, in)
}

func (f *PacketFilter) match(
	ctx context.Context,
	in packetfiltercondition.Input,
) (_ret bool) {
	defer func() {
		if !_ret {
			f.TotalDroppedPackets += 1
			if in.Input.Size() > 0 {
				f.TotalDroppedBytes += uint64(in.Input.Size())
			}
		}
	}()
	streamIndex := in.Input.GetStreamIndex()
	isKeyFrame := in.Input.Packet.Flags().Has(astiav.PacketFlagKey)
	if isKeyFrame {
		delete(f.WaitingForKeyFrame, streamIndex)
		return true
	}
	if _, ok := f.WaitingForKeyFrame[streamIndex]; ok {
		return false
	}
	if f.Condition != nil && f.Condition.Match(ctx, in) {
		return true
	}
	f.WaitingForKeyFrame[streamIndex] = struct{}{}
	return false
}
