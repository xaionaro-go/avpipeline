package node

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node/filter"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type PushToGeneric[T any, C filter.Condition[T]] struct {
	Node      Abstract
	Condition C
}

type PushTo = PushToGeneric[packetorframe.InputUnion, packetorframefiltercondition.Condition]

type PushTos []PushTo

func pushToConds(conds ...packetorframefiltercondition.Condition) packetorframefiltercondition.Condition {
	switch len(conds) {
	case 0:
		return nil
	case 1:
		return conds[0]
	}
	return packetorframefiltercondition.And(conds)
}

func (s *PushTos) Add(dst Abstract, conds ...packetorframefiltercondition.Condition) *PushTos {
	*s = append(*s, PushTo{
		Node:      dst,
		Condition: pushToConds(conds...),
	})
	return s
}

func (s PushTos) Nodes() Nodes[Abstract] {
	var result Nodes[Abstract]
	for _, item := range s {
		result = append(result, item.Node)
	}
	return result
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

func frameConds(conds ...framefiltercondition.Condition) framefiltercondition.Condition {
	switch len(conds) {
	case 0:
		return nil
	case 1:
		return conds[0]
	}
	return framefiltercondition.And(conds)
}

func (s *PushFramesTos) Add(dst Abstract, conds ...framefiltercondition.Condition) *PushFramesTos {
	*s = append(*s, PushFramesTo{
		Node:      dst,
		Condition: frameConds(conds...),
	})
	return s
}

func (s PushFramesTos) Nodes() Nodes[Abstract] {
	var result Nodes[Abstract]
	for _, item := range s {
		result = append(result, item.Node)
	}
	return result
}

type PushPacketsTo = PushToGeneric[packet.Input, packetfiltercondition.Condition]

type PushPacketsTos []PushPacketsTo

func packetConds(conds ...packetfiltercondition.Condition) packetfiltercondition.Condition {
	switch len(conds) {
	case 0:
		return nil
	case 1:
		return conds[0]
	}
	return packetfiltercondition.And(conds)
}

func (s *PushPacketsTos) Add(dst Abstract, conds ...packetfiltercondition.Condition) *PushPacketsTos {
	*s = append(*s, PushPacketsTo{
		Node:      dst,
		Condition: packetConds(conds...),
	})
	return s
}

func (s PushPacketsTos) Nodes() Nodes[Abstract] {
	var result Nodes[Abstract]
	for _, item := range s {
		result = append(result, item.Node)
	}
	return result
}

func (s PushPacketsTos) Contains(pushTo PushPacketsTo) bool {
	for _, item := range s {
		if item == pushTo {
			return true
		}
	}
	return false
}
