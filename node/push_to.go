// push_to.go defines the PushTo structure for connecting nodes.

package node

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node/filter"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	conditionbase "github.com/xaionaro-go/avpipeline/types/condition"
)

type PushToGeneric[T any, C filter.Condition[T]] struct {
	Node      Abstract
	Condition C
}

type PushTo = PushToGeneric[packetorframe.InputUnion, packetorframefiltercondition.Condition]

type PushTos []PushTo

func (s *PushTos) Add(dst Abstract, conds ...packetorframefiltercondition.Condition) *PushTos {
	*s = append(*s, PushTo{
		Node:      dst,
		Condition: conditionbase.CombineConds(conds...),
	})
	return s
}

func (s PushTos) Nodes() Nodes[Abstract] {
	return nodesFromPushTos(s)
}

func (s PushTos) Contains(pushTo PushTo) bool {
	for _, item := range s {
		if item == pushTo {
			return true
		}
	}
	return false
}

type PushFramesTo = PushToGeneric[frame.Input, framefiltercondition.Condition]

type PushFramesTos []PushFramesTo

func (s *PushFramesTos) Add(dst Abstract, conds ...framefiltercondition.Condition) *PushFramesTos {
	*s = append(*s, PushFramesTo{
		Node:      dst,
		Condition: conditionbase.CombineConds(conds...),
	})
	return s
}

func (s PushFramesTos) Nodes() Nodes[Abstract] {
	return nodesFromPushTos(s)
}

func (s PushFramesTos) Contains(pushTo PushFramesTo) bool {
	for _, item := range s {
		if item == pushTo {
			return true
		}
	}
	return false
}

type PushPacketsTo = PushToGeneric[packet.Input, packetfiltercondition.Condition]

type PushPacketsTos []PushPacketsTo

func (s *PushPacketsTos) Add(dst Abstract, conds ...packetfiltercondition.Condition) *PushPacketsTos {
	*s = append(*s, PushPacketsTo{
		Node:      dst,
		Condition: conditionbase.CombineConds(conds...),
	})
	return s
}

func (s PushPacketsTos) Nodes() Nodes[Abstract] {
	return nodesFromPushTos(s)
}

func (s PushPacketsTos) Contains(pushTo PushPacketsTo) bool {
	for _, item := range s {
		if item == pushTo {
			return true
		}
	}
	return false
}

type nodeGetter interface {
	getNode() Abstract
}

func (p PushToGeneric[T, C]) getNode() Abstract {
	return p.Node
}

func nodesFromPushTos[T nodeGetter](pushTos []T) Nodes[Abstract] {
	var result Nodes[Abstract]
	for _, item := range pushTos {
		result = append(result, item.getNode())
	}
	return result
}
