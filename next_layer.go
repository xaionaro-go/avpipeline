// next_layer.go provides functionality for traversing the next layer of nodes in the media pipeline.

package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func NextLayer[T node.Abstract](
	ctx context.Context,
	nodes ...T,
) ([]node.Abstract, error) {
	preResult, err := nextLayer(ctx, nodes...)

	var result []node.Abstract
	isSet := map[node.Abstract]struct{}{}
	for _, r := range preResult {
		n := r.Node
		if _, ok := isSet[n]; ok {
			continue
		}
		isSet[n] = struct{}{}
		result = append(result, n)
	}

	return result, err
}

type nodeAbstractWithItemType struct {
	Node     node.Abstract
	ItemType reflect.Type
}

func nextLayer[T node.Abstract](
	ctx context.Context,
	nodes ...T,
) ([]nodeAbstractWithItemType, error) {
	isSet := map[nodeAbstractWithItemType]struct{}{}
	var nextNodes []nodeAbstractWithItemType
	var errs []error
	for _, n := range nodes {
		for _, pushTo := range n.GetPushTos(ctx) {
			r := nodeAbstractWithItemType{
				Node:     pushTo.Node,
				ItemType: reflect.TypeOf((*packetorframe.InputUnion)(nil)),
			}
			if _, ok := isSet[r]; ok {
				continue
			}
			isSet[r] = struct{}{}
			if pushTo.Node == nil {
				errs = append(errs, fmt.Errorf("received a nil node (PushTo)"))
				continue
			}
			nextNodes = append(nextNodes, r)
		}
	}
	return nextNodes, errors.Join(errs...)
}
