package goconv

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
		Description: fmt.Sprintf("%s", n),
		IsServing:   n.IsServing(),
		Counters:    NodeCountersToGRPC(n.GetCountersPtr(), n.GetProcessor().CountersPtr()),
	}

	var currentLayer []node.Abstract
	for {
		currentLayer = append(currentLayer, n)
		origer, ok := n.(interface{ OriginalNodeAbstract() node.Abstract })
		if !ok {
			break
		}
		n = origer.OriginalNodeAbstract()
	}

	nextLayer, err := avpipeline.NextLayer(ctx, currentLayer...)
	if err != nil {
		panic(err)
	}
	for _, nextNode := range nextLayer {
		result.ConsumingNodes = append(result.ConsumingNodes, NodeToGRPC(ctx, nextNode))
	}

	return result
}
