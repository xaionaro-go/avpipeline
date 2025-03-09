package avpipeline

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/asticode/go-astiav"
)

type ProcessingNode interface {
	fmt.Stringer
	io.Closer
	GetOutputFormatContext(ctx context.Context) *astiav.FormatContext
	SendPacketChan() chan<- InputPacket
	OutputPacketsChan() <-chan OutputPacket
	ErrorChan() <-chan error
}

type Pipeline struct {
	CommonsProcessing
	ProcessingNode
	PushTo []*Pipeline
}

func (p *Pipeline) String() string {
	if p == nil {
		return "<nil>"
	}

	var pushToStrs []string
	for _, pushTo := range p.PushTo {
		pushToStrs = append(pushToStrs, pushTo.String())
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
