package avpipeline

import (
	"github.com/xaionaro-go/avpipeline/condition"
)

type PushTo struct {
	Node      AbstractNode
	Condition condition.Condition
}

type PushTos []PushTo

func (s *PushTos) Add(dst AbstractNode, conds ...condition.Condition) *PushTos {
	var cond condition.Condition
	switch len(conds) {
	case 0:
		break
	case 1:
		cond = conds[0]
	case 2:
		cond = condition.And(conds)
	}
	*s = append(*s, PushTo{
		Node:      dst,
		Condition: cond,
	})
	return s
}
