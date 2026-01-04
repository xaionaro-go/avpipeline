// find.go provides functionality for finding nodes in the media pipeline.

package avpipeline

import (
	"context"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/avpipeline/node"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type ErrNotFound struct{}

func (e ErrNotFound) Error() string {
	return "not found"
}

func FindNodeByObjectID(
	ctx context.Context,
	objID globaltypes.ObjectID,
	nodes ...node.Abstract,
) (_ret node.Abstract, _err error) {
	defer func() {
		if _err == nil {
			assert(ctx, _ret.GetObjectID() == objID, fmt.Sprintf("found an invalid node: %v != %v", _ret.GetObjectID(), objID))
		}
	}()
	var callback func(ctx context.Context, parent node.Abstract, item reflect.Type, n node.Abstract) error
	callback = func(ctx context.Context, parent node.Abstract, item reflect.Type, n node.Abstract) error {
		if n.GetObjectID() == objID {
			_ret = n
			return ErrTraverseStop{}
		}
		if n, ok := n.(node.OriginalNodeAbstracter); ok {
			_err = Traverse(ctx, callback, n.OriginalNodeAbstract())
			if _err != nil {
				return _err
			}
			if _ret != nil {
				return ErrTraverseStop{}
			}
		}
		return nil
	}
	_err = Traverse(ctx, callback, nodes...)
	if _err == nil && _ret == nil {
		_err = ErrNotFound{}
	}
	return
}
