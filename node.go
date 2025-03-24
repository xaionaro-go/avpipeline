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
)

type AbstractNode interface {
	fmt.Stringer
	DotString(bool) string

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

type Node[T processor.Abstract] struct {
	*NodeStatistics
	Processor            T
	PushPacketsTo        PushPacketsTos
	PushFramesTo         PushFramesTos
	InputPacketCondition packetcondition.Condition
	InputFrameCondition  framecondition.Condition
}

var _ AbstractNode = (*Node[processor.Abstract])(nil)

func NewNode[T processor.Abstract](processor T) *Node[T] {
	return &Node[T]{
		NodeStatistics: &NodeStatistics{},
		Processor:      processor,
	}
}

func NewNodeFromKernel[T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...processor.Option,
) *Node[*processor.FromKernel[T]] {
	return NewNode(
		processor.NewFromKernel(
			ctx,
			kernel,
			opts...,
		),
	)
}

func (n *Node[T]) GetStatistics() *NodeStatistics {
	return n.NodeStatistics
}

func (n *Node[T]) GetProcessor() processor.Abstract {
	return n.Processor
}

func (n *Node[T]) GetPushPacketsTos() PushPacketsTos {
	return n.PushPacketsTo
}

func (n *Node[T]) AddPushPacketsTo(dst AbstractNode, conds ...packetcondition.Condition) {
	n.PushPacketsTo.Add(dst, conds...)
}

func (n *Node[T]) SetPushPacketsTos(s PushPacketsTos) {
	n.PushPacketsTo = s
}

func (n *Node[T]) GetPushFramesTos() PushFramesTos {
	return n.PushFramesTo
}

func (n *Node[T]) AddPushFramesTo(dst AbstractNode, conds ...framecondition.Condition) {
	n.PushFramesTo.Add(dst, conds...)
}

func (n *Node[T]) SetPushFramesTos(s PushFramesTos) {
	n.PushFramesTo = s
}

func (n *Node[T]) GetInputPacketCondition() packetcondition.Condition {
	return n.InputPacketCondition
}

func (n *Node[T]) SetInputPacketCondition(cond packetcondition.Condition) {
	n.InputPacketCondition = cond
}

func (n *Node[T]) GetInputFrameCondition() framecondition.Condition {
	return n.InputFrameCondition
}

func (n *Node[T]) SetInputFrameCondition(cond framecondition.Condition) {
	n.InputFrameCondition = cond
}

func (n *Node[T]) String() string {
	if n == nil {
		return "<nil>"
	}

	var pushToStrs []string
	for _, pushTo := range n.PushPacketsTo {
		pushToStrs = append(pushToStrs, pushTo.Node.GetProcessor().String())
	}

	switch len(pushToStrs) {
	case 0:
		return n.Processor.String()
	case 1:
		return fmt.Sprintf("%s -> %s", n.Processor, pushToStrs[0])
	default:
		return fmt.Sprintf("%s -> {%s}", n.Processor, strings.Join(pushToStrs, ", "))
	}
}

func (n *Node[T]) DotString(withStats bool) string {
	if withStats {
		panic("not implemented, yet")
	}
	var result strings.Builder
	fmt.Fprintf(&result, "digraph Pipeline {\n")
	alreadyPrinted := map[processor.Abstract]struct{}{}
	n.dotBlockContentStringWriteTo(&result, alreadyPrinted)
	fmt.Fprintf(&result, "}\n")
	return result.String()
}

func (n *Node[T]) dotBlockContentStringWriteTo(
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
		writer, ok := pushTo.Node.(interface {
			dotBlockContentStringWriteTo(io.Writer, map[processor.Abstract]struct{})
		})
		if !ok {
			continue
		}
		writer.dotBlockContentStringWriteTo(w, alreadyPrinted)
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
		writer, ok := pushTo.Node.(interface {
			dotBlockContentStringWriteTo(io.Writer, map[processor.Abstract]struct{})
		})
		if !ok {
			continue
		}
		writer.dotBlockContentStringWriteTo(w, alreadyPrinted)
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
