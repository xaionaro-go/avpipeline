package node

import (
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node/filter"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packet"
)

type PushTo[T any, C filter.Condition[T]] struct {
	Node      Abstract
	Condition C
}

type PushFramesTo = PushTo[frame.Input, framefiltercondition.Condition]

type PushFramesTos []PushFramesTo

func (s *PushFramesTos) Add(dst Abstract, conds ...framefiltercondition.Condition) *PushFramesTos {
	var cond framefiltercondition.Condition
	switch len(conds) {
	case 0:
		break
	case 1:
		cond = conds[0]
	case 2:
		cond = framefiltercondition.And(conds)
	}
	*s = append(*s, PushFramesTo{
		Node:      dst,
		Condition: cond,
	})
	return s
}

type PushPacketsTo = PushTo[packet.Input, packetfiltercondition.Condition]

type PushPacketsTos []PushPacketsTo

func (s *PushPacketsTos) Add(dst Abstract, conds ...packetfiltercondition.Condition) *PushPacketsTos {
	var cond packetfiltercondition.Condition
	switch len(conds) {
	case 0:
		break
	case 1:
		cond = conds[0]
	case 2:
		cond = packetfiltercondition.And(conds)
	}
	*s = append(*s, PushPacketsTo{
		Node:      dst,
		Condition: cond,
	})
	return s
}
