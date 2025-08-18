package goconv

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/node"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func NodeToGRPC(n node.Abstract) *avpipelinegrpc.Node {
	if n == nil {
		return nil
	}
	result := &avpipelinegrpc.Node{
		Id:          fmt.Sprintf("%p", n),
		Type:        fmt.Sprintf("%T", n),
		Description: fmt.Sprintf("%s", n),
		IsServing:   n.IsServing(),
		Statistics:  NodeStatisticsToGRPC(n.GetStatistics()),
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

	nextLayer, err := avpipeline.NextLayer(currentLayer...)
	if err != nil {
		panic(err)
	}
	for _, nextNode := range nextLayer {
		result.ConsumingNodes = append(result.ConsumingNodes, NodeToGRPC(nextNode))
	}

	return result
}
