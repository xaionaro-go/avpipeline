// node.go provides conversion functions for node between Protobuf and Go.

// Package avpipeline provides conversion functions between Protobuf and Go for avpipeline types.
package avpipeline

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/node"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func NodeToGRPC(
	ctx context.Context,
	n node.Abstract,
) *avpipelinegrpc.Node {
	if n == nil {
		return nil
	}
	result := &avpipelinegrpc.Node{
		Id:          uint64(n.GetObjectID()),
		Type:        fmt.Sprintf("%T", n),
		Description: n.String(),
		IsServing:   n.IsServing(),
		Counters:    NodeCountersToGRPC(n.GetCountersPtr(), n.GetProcessor().CountersPtr()),
	}

	for {
		origer, ok := n.(interface{ OriginalNodeAbstract() node.Abstract })
		if !ok {
			break
		}
		nextN := origer.OriginalNodeAbstract()
		if nextN == nil {
			break
		}
		n = nextN
	}

	nextLayer, err := avpipeline.NextLayer(ctx, n)
	if err != nil {
		panic(err)
	}

	for _, nextNode := range nextLayer {
		result.ConsumingNodes = append(result.ConsumingNodes, NodeToGRPC(ctx, nextNode))
	}

	return result
}
