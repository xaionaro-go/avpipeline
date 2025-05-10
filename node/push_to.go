package node

import (
	"github.com/xaionaro-go/avpipeline/frame"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/types"
)

type PushTo[T any, C types.Condition[T]] struct {
	Node      Abstract
	Condition C
}

type PushFramesTo = PushTo[frame.Input, types.Condition[frame.Input]]

type PushFramesTos []PushFramesTo

func (s *PushFramesTos) Add(dst Abstract, conds ...framecondition.Condition) *PushFramesTos {
	var cond framecondition.Condition
	switch len(conds) {
	case 0:
		break
	case 1:
		cond = conds[0]
	case 2:
		cond = framecondition.And(conds)
	}
	*s = append(*s, PushFramesTo{
		Node:      dst,
		Condition: cond,
	})
	return s
}

type PushPacketsTo = PushTo[packet.Input, types.Condition[packet.Input]]

type PushPacketsTos []PushPacketsTo

func (s *PushPacketsTos) Add(dst Abstract, conds ...packetcondition.Condition) *PushPacketsTos {
	var cond packetcondition.Condition
	switch len(conds) {
	case 0:
		break
	case 1:
		cond = conds[0]
	case 2:
		cond = packetcondition.And(conds)
	}
	*s = append(*s, PushPacketsTo{
		Node:      dst,
		Condition: cond,
	})
	return s
}
