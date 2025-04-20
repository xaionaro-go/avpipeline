package avpipeline

import (
	"fmt"
	"io"
	"strings"

	"github.com/xaionaro-go/avpipeline/processor"
)

type Nodes[T AbstractNode] []T

func (s Nodes[T]) String() string {
	var results []string
	for _, n := range s {
		isSet := map[AbstractNode]struct{}{}
		var pushToStrs []string
		for _, pushTo := range n.GetPushPacketsTos() {
			if _, ok := isSet[pushTo.Node]; ok {
				continue
			}
			isSet[pushTo.Node] = struct{}{}
			pushToStrs = append(pushToStrs, pushTo.Node.GetProcessor().String())
		}
		for _, pushTo := range n.GetPushFramesTos() {
			if _, ok := isSet[pushTo.Node]; ok {
				continue
			}
			isSet[pushTo.Node] = struct{}{}
			pushToStrs = append(pushToStrs, pushTo.Node.GetProcessor().String())
		}

		switch len(pushToStrs) {
		case 0:
			results = append(results, n.GetProcessor().String())
		case 1:
			results = append(results, fmt.Sprintf("%s -> %s", n.GetProcessor(), pushToStrs[0]))
		default:
			results = append(results, fmt.Sprintf("%s -> {%s}", n.GetProcessor(), strings.Join(pushToStrs, ", ")))
		}
	}
	switch len(results) {
	case 0:
		return "{}"
	case 1:
		return results[0]
	default:
		return fmt.Sprintf("{%s}", strings.Join(results, ", "))
	}
}

func (s Nodes[T]) DotString(withStats bool) string {
	if withStats {
		panic("not implemented, yet")
	}
	var result strings.Builder
	fmt.Fprintf(&result, "digraph Pipeline {\n")
	alreadyPrinted := map[processor.Abstract]struct{}{}
	for _, n := range s {
		w, ok := AbstractNode(n).(DotBlockContentStringWriteToer)
		if !ok {
			continue
		}
		w.DotBlockContentStringWriteTo(&result, alreadyPrinted)
	}
	fmt.Fprintf(&result, "}\n")
	return result.String()
}

type DotBlockContentStringWriteToer interface {
	DotBlockContentStringWriteTo(
		w io.Writer,
		alreadyPrinted map[processor.Abstract]struct{},
	)
}
