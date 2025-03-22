package avpipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/processor"
)

type Node struct {
	NodeStatistics
	Processor processor.Abstract
	PushTo    PushTos
}

func NewNode(processor processor.Abstract) *Node {
	return &Node{
		Processor: processor,
	}
}

func NewNodeFromKernel(
	ctx context.Context,
	kernel kernel.Abstract,
	opts ...processor.Option,
) *Node {
	return NewNode(
		processor.NewFromKernel(
			ctx,
			kernel,
			opts...,
		),
	)
}

func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}

	var pushToStrs []string
	for _, pushTo := range n.PushTo {
		pushToStrs = append(pushToStrs, pushTo.Processor.String())
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

func (n *Node) DotString(withStats bool) string {
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

func (n *Node) dotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if _, ok := alreadyPrinted[n.Processor]; !ok {
		fmt.Fprintf(
			w,
			"\tnode_%p [label="+`"%s"`+"]\n",
			n.Processor,
			strings.ReplaceAll(n.Processor.String(), `"`, ``),
		)
		alreadyPrinted[n.Processor] = struct{}{}
	}
	for _, pushTo := range n.PushTo {
		pushTo.Node.dotBlockContentStringWriteTo(w, alreadyPrinted)
		if pushTo.Condition == nil {
			fmt.Fprintf(w, "\tnode_%p -> node_%p\n", n.Processor, pushTo.Processor)
			continue
		}
		fmt.Fprintf(
			w,
			"\tnode_%p -> node_%p [label="+`"%s"`+"]\n",
			n.Processor,
			pushTo.Processor,
			strings.ReplaceAll(pushTo.Condition.String(), `"`, ``),
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
