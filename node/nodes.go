// nodes.go defines a collection type for nodes.

package node

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/xaionaro-go/avpipeline/processor"
)

type Nodes[T Abstract] []T

func (s Nodes[T]) String() string {
	var results []string
	for _, n := range s {
		results = append(results, n.GetProcessor().String())
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

func origNode(n Abstract) Abstract {
	if origer, ok := n.(interface{ OriginalNodeAbstract() Abstract }); ok {
		return origer.OriginalNodeAbstract()
	}
	return n
}

func (s Nodes[T]) StringRecursive() string {
	ctx := context.Background()
	var results []string
	for _, n := range s {
		isSet := map[Abstract]struct{}{}
		var pushToStrs []string
		origNode(n).WithPushTos(ctx, func(ctx context.Context, pushTos *PushTos) {
			for _, pushTo := range *pushTos {
				if _, ok := isSet[pushTo.Node]; ok {
					continue
				}
				isSet[pushTo.Node] = struct{}{}
				pushToStrs = append(pushToStrs, pushTo.Node.GetProcessor().String())
			}
		})
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
		w, ok := Abstract(n).(DotBlockContentStringWriteToer)
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

func (s Nodes[T]) Without(in Nodes[T]) Nodes[T] {
	var result Nodes[T]
	for _, n := range s {
		found := false
		for _, i := range in {
			if any(n) == any(i) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, n)
		}
	}
	return result
}
