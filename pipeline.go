package avpipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/condition"
)

type ProcessingNode interface {
	fmt.Stringer
	io.Closer
	GetOutputFormatContext(ctx context.Context) *astiav.FormatContext
	SendPacketChan() chan<- InputPacket
	OutputPacketsChan() <-chan OutputPacket
	ErrorChan() <-chan error
}

type PushTo struct {
	*Pipeline
	Condition Condition
}

type PushTos []PushTo

func (s *PushTos) Add(p *Pipeline, conds ...Condition) *PushTos {
	var cond Condition
	switch len(conds) {
	case 0:
		break
	case 1:
		cond = conds[0]
	case 2:
		cond = condition.And(conds)
	}
	*s = append(*s, PushTo{
		Pipeline:  p,
		Condition: cond,
	})
	return s
}

type Pipeline struct {
	CommonsProcessing
	ProcessingNode
	PushTo PushTos
}

func (p *Pipeline) String() string {
	if p == nil {
		return "<nil>"
	}

	var pushToStrs []string
	for _, pushTo := range p.PushTo {
		pushToStrs = append(pushToStrs, pushTo.ProcessingNode.String())
	}

	switch len(pushToStrs) {
	case 0:
		return p.ProcessingNode.String()
	case 1:
		return fmt.Sprintf("%s -> %s", p.ProcessingNode, pushToStrs[0])
	default:
		return fmt.Sprintf("%s -> {%s}", p.ProcessingNode, strings.Join(pushToStrs, ", "))
	}
}

func (p *Pipeline) DotString(withStats bool) string {
	if withStats {
		panic("not implemented, yet")
	}
	var result strings.Builder
	fmt.Fprintf(&result, "digraph Pipeline {\n")
	alreadyPrinted := map[ProcessingNode]struct{}{}
	p.dotBlockContentStringWriteTo(&result, alreadyPrinted)
	fmt.Fprintf(&result, "}\n")
	return result.String()
}

func (p *Pipeline) dotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[ProcessingNode]struct{},
) {
	if _, ok := alreadyPrinted[p.ProcessingNode]; !ok {
		fmt.Fprintf(
			w,
			"\tnode_%p [label="+`"%s"`+"]\n",
			p.ProcessingNode,
			strings.ReplaceAll(p.ProcessingNode.String(), `"`, ``),
		)
		alreadyPrinted[p.ProcessingNode] = struct{}{}
	}
	for _, pushTo := range p.PushTo {
		pushTo.Pipeline.dotBlockContentStringWriteTo(w, alreadyPrinted)
		if pushTo.Condition == nil {
			fmt.Fprintf(w, "\tnode_%p -> node_%p\n", p.ProcessingNode, pushTo.ProcessingNode)
			continue
		}
		fmt.Fprintf(
			w,
			"\tnode_%p -> node_%p [label="+`"%s"`+"]\n",
			p.ProcessingNode,
			pushTo.ProcessingNode,
			strings.ReplaceAll(pushTo.Condition.String(), `"`, ``),
		)
	}
}

func NewPipelineNode(processingNode ProcessingNode) *Pipeline {
	return &Pipeline{
		ProcessingNode: processingNode,
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
