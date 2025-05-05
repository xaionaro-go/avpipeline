package avpipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type AbstractNode interface {
	Serve(context.Context, ServeConfig, chan<- ErrNode)

	GetPushPacketsTos() PushPacketsTos
	AddPushPacketsTo(dst AbstractNode, conds ...packetcondition.Condition)
	SetPushPacketsTos(PushPacketsTos)
	GetPushFramesTos() PushFramesTos
	AddPushFramesTo(dst AbstractNode, conds ...framecondition.Condition)
	SetPushFramesTos(PushFramesTos)

	GetStatistics() *NodeStatistics
	GetProcessor() processor.Abstract

	GetInputPacketCondition() packetcondition.Condition
	SetInputPacketCondition(packetcondition.Condition)
	GetInputFrameCondition() framecondition.Condition
	SetInputFrameCondition(framecondition.Condition)
}

type NodeWithCustomData[C any, T processor.Abstract] struct {
	*NodeStatistics
	Processor            T
	PushPacketsTo        PushPacketsTos
	PushFramesTo         PushFramesTos
	InputPacketCondition packetcondition.Condition
	InputFrameCondition  framecondition.Condition
	Locker               xsync.Mutex

	CustomData C
}

type Node[T processor.Abstract] = NodeWithCustomData[struct{}, T]

var _ AbstractNode = (*Node[processor.Abstract])(nil)

func NewNode[T processor.Abstract](processor T) *Node[T] {
	return NewNodeWithCustomData[struct{}](processor)
}

func NewNodeFromKernel[T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...processor.Option,
) *Node[*processor.FromKernel[T]] {
	return NewNodeWithCustomDataFromKernel[struct{}](ctx, kernel, opts...)
}

func NewNodeWithCustomData[C any, T processor.Abstract](
	processor T,
) *NodeWithCustomData[C, T] {
	return &NodeWithCustomData[C, T]{
		NodeStatistics: &NodeStatistics{},
		Processor:      processor,
	}
}

func NewNodeWithCustomDataFromKernel[C any, T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...processor.Option,
) *NodeWithCustomData[C, *processor.FromKernel[T]] {
	return NewNodeWithCustomData[C](
		processor.NewFromKernel(
			ctx,
			kernel,
			opts...,
		),
	)
}

func (n *NodeWithCustomData[C, T]) GetStatistics() *NodeStatistics {
	return xsync.DoR1(context.TODO(), &n.Locker, func() *NodeStatistics {
		return n.NodeStatistics
	})
}

func (n *NodeWithCustomData[C, T]) GetProcessor() processor.Abstract {
	return xsync.DoR1(context.TODO(), &n.Locker, func() processor.Abstract {
		return n.Processor
	})
}

func (n *NodeWithCustomData[C, T]) GetPushPacketsTos() PushPacketsTos {
	return xsync.DoR1(context.TODO(), &n.Locker, func() PushPacketsTos {
		return n.PushPacketsTo
	})
}

func (n *NodeWithCustomData[C, T]) AddPushPacketsTo(
	dst AbstractNode,
	conds ...packetcondition.Condition,
) {
	n.Locker.Do(context.TODO(), func() {
		n.PushPacketsTo.Add(dst, conds...)
	})
}

func (n *NodeWithCustomData[C, T]) SetPushPacketsTos(s PushPacketsTos) {
	n.Locker.Do(context.TODO(), func() {
		n.PushPacketsTo = s
	})
}

func (n *NodeWithCustomData[C, T]) GetPushFramesTos() PushFramesTos {
	return xsync.DoR1(context.TODO(), &n.Locker, func() PushFramesTos {
		return n.PushFramesTo
	})
}

func (n *NodeWithCustomData[C, T]) AddPushFramesTo(
	dst AbstractNode,
	conds ...framecondition.Condition,
) {
	n.Locker.Do(context.TODO(), func() {
		n.PushFramesTo.Add(dst, conds...)
	})
}

func (n *NodeWithCustomData[C, T]) SetPushFramesTos(s PushFramesTos) {
	n.Locker.Do(context.TODO(), func() {
		n.PushFramesTo = s
	})
}

func (n *NodeWithCustomData[C, T]) GetInputPacketCondition() packetcondition.Condition {
	return xsync.DoR1(context.TODO(), &n.Locker, func() packetcondition.Condition {
		return n.InputPacketCondition
	})
}

func (n *NodeWithCustomData[C, T]) SetInputPacketCondition(cond packetcondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputPacketCondition = cond
	})
}

func (n *NodeWithCustomData[C, T]) GetInputFrameCondition() framecondition.Condition {
	return xsync.DoR1(context.TODO(), &n.Locker, func() framecondition.Condition {
		return n.InputFrameCondition
	})
}

func (n *NodeWithCustomData[C, T]) SetInputFrameCondition(cond framecondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputFrameCondition = cond
	})
}

func (n *NodeWithCustomData[C, T]) String() string {
	return Nodes[*NodeWithCustomData[C, T]]{n}.String()
}

func (n *NodeWithCustomData[C, T]) DotString(withStats bool) string {
	return Nodes[*NodeWithCustomData[C, T]]{n}.DotString(withStats)
}

func (n *NodeWithCustomData[C, T]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	n.dotBlockContentStringWriteTo(w, alreadyPrinted)
}

func (n *NodeWithCustomData[C, T]) dotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	sanitizeString := func(s string) string {
		s = strings.ReplaceAll(s, `"`, ``)
		s = strings.ReplaceAll(s, "\n", `\n`)
		s = strings.ReplaceAll(s, "\t", ``)
		return s
	}

	if _, ok := alreadyPrinted[n.Processor]; !ok {
		fmt.Fprintf(
			w,
			"\tnode_%p [label="+`"%s"`+"]\n",
			any(n.Processor),
			sanitizeString(n.Processor.String()),
		)
		alreadyPrinted[n.Processor] = struct{}{}
	}
	for _, pushTo := range n.PushPacketsTo {
		writer, ok := pushTo.Node.(DotBlockContentStringWriteToer)
		if !ok {
			continue
		}
		writer.DotBlockContentStringWriteTo(w, alreadyPrinted)
		if pushTo.Condition == nil {
			fmt.Fprintf(w, "\tnode_%p -> node_%p\n", any(n.Processor), pushTo.Node.GetProcessor())
			continue
		}
		fmt.Fprintf(
			w,
			"\tnode_%p -> node_%p [label="+`"%s"`+"]\n",
			any(n.Processor),
			pushTo.Node.GetProcessor(),
			sanitizeString(n.Processor.String()),
		)
	}
	for _, pushTo := range n.PushFramesTo {
		writer, ok := pushTo.Node.(DotBlockContentStringWriteToer)
		if !ok {
			continue
		}
		writer.DotBlockContentStringWriteTo(w, alreadyPrinted)
		if pushTo.Condition == nil {
			fmt.Fprintf(w, "\tnode_%p -> node_%p\n", any(n.Processor), pushTo.Node.GetProcessor())
			continue
		}
		fmt.Fprintf(
			w,
			"\tnode_%p -> node_%p [label="+`"%s"`+"]\n",
			any(n.Processor),
			pushTo.Node.GetProcessor(),
			sanitizeString(n.Processor.String()),
		)
	}
}
