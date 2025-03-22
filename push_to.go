package avpipeline

import (
	"github.com/xaionaro-go/avpipeline/condition"
)

type PushTo struct {
	*Node
	Condition condition.Condition
}

type PushTos []PushTo

func (s *PushTos) Add(p *Node, conds ...condition.Condition) *PushTos {
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
		Node:      p,
		Condition: cond,
	})
	return s
}
