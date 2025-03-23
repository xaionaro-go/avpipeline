package avpipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/processor"
)

type AbstractNode interface {
	fmt.Stringer
	DotString(bool) string
	GetStatistics() *NodeStatistics
	GetProcessor() processor.Abstract
	GetPushTos() PushTos
	AddPushTo(dst AbstractNode, conds ...condition.Condition)
	SetPushTos(PushTos)
	GetInputCondition() condition.Condition
	SetInputCondition(condition.Condition)
	Serve(context.Context, ServeConfig, chan<- ErrNode)
}

type Node[T processor.Abstract] struct {
	*NodeStatistics
	Processor      T
	PushTo         PushTos
	InputCondition condition.Condition
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

func (n *Node[T]) GetPushTos() PushTos {
	return n.PushTo
}

func (n *Node[T]) AddPushTo(dst AbstractNode, conds ...condition.Condition) {
	n.PushTo.Add(dst, conds...)
}

func (n *Node[T]) SetPushTos(s PushTos) {
	n.PushTo = s
}

func (n *Node[T]) GetInputCondition() condition.Condition {
	return n.InputCondition
}

func (n *Node[T]) SetInputCondition(cond condition.Condition) {
	n.InputCondition = cond
}

func (n *Node[T]) String() string {
	if n == nil {
		return "<nil>"
	}

	var pushToStrs []string
	for _, pushTo := range n.PushTo {
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
	for _, pushTo := range n.PushTo {
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

func getOutputStream(
	_ context.Context,
	fmtCtx *astiav.FormatContext,
	streamIndex int,
) *astiav.Stream {
	for _, stream := range fmtCtx.Streams() {
		if stream.Index() == streamIndex {
			return stream
		}
	}
	return nil
}
