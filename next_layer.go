package avpipeline

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
)

func NextLayer[T node.Abstract](nodes ...T) ([]node.Abstract, error) {
	preResult, err := nextLayer(nodes...)

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

func nextLayer[T node.Abstract](nodes ...T) ([]nodeAbstractWithItemType, error) {
	isSet := map[nodeAbstractWithItemType]struct{}{}
	var nextNodes []nodeAbstractWithItemType
	var errs []error
	for _, n := range nodes {
		for _, pushTo := range n.GetPushPacketsTos() {
			r := nodeAbstractWithItemType{
				Node:     n,
				ItemType: reflect.TypeOf((*packet.Input)(nil)),
			}
			if _, ok := isSet[r]; ok {
				continue
			}
			isSet[r] = struct{}{}
			if pushTo.Node == nil {
				errs = append(errs, fmt.Errorf("received a nil node"))
				continue
			}
			nextNodes = append(nextNodes, r)
		}
		for _, pushTo := range n.GetPushFramesTos() {
			r := nodeAbstractWithItemType{
				Node:     n,
				ItemType: reflect.TypeOf((*frame.Input)(nil)),
			}
			if _, ok := isSet[r]; ok {
				continue
			}
			isSet[r] = struct{}{}
			if pushTo.Node == nil {
				errs = append(errs, fmt.Errorf("received a nil node"))
				continue
			}
			nextNodes = append(nextNodes, r)
		}
	}
	return nextNodes, errors.Join(errs...)
}
