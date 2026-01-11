// filter.go implements a packet filter that removes filler packets.

// Package removefiller provides a packet filter that removes filler packets.
package removefiller

import (
	"context"

	extradatapacket "github.com/xaionaro-go/avpipeline/extradata/packet"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

// Filter implements the packet.Filter interface (via condition.Condition).
type Filter struct{}

var _ condition.Condition = (*Filter)(nil)

// New creates a new RemoveFiller filter.
func New() *Filter {
	return &Filter{}
}

// String returns a string representation of the filter.
func (f *Filter) String() string {
	return "RemoveFiller"
}

// Match returns true if the packet should be KEPT. It strips filler NALUs from the packet.
func (f *Filter) Match(
	ctx context.Context,
	pkt packet.Input,
) bool {
	cp := pkt.GetCodecParameters()
	if cp == nil {
		return true
	}

	data := pkt.Data()
	if len(data) == 0 {
		return true
	}

	var keptNALUs []extradatapacket.NALU
	removed := false
	for nalu, err := range extradatapacket.Iter(cp.CodecID(), data) {
		if err != nil {
			return true
		}
		if extradatapacket.IsFiller(cp.CodecID(), nalu.Type) {
			removed = true
			continue
		}
		keptNALUs = append(keptNALUs, nalu)
	}

	if !removed {
		return true
	}

	if len(keptNALUs) == 0 {
		return false
	}

	newData := extradatapacket.JoinNALUs(keptNALUs)
	err := pkt.FromData(newData)
	if err != nil {
		return true
	}

	return true
}
