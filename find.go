package avpipeline

import (
	"context"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/avpipeline/node"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

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
	_err = Traverse(
		ctx,
		func(ctx context.Context, parent node.Abstract, item reflect.Type, n node.Abstract) error {
			if n.GetObjectID() == objID {
				_ret = n
				return ErrTraverseStop{}
			}
			return nil
		},
		nodes...,
	)
	return
}
