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

// firstFrameUnixNanoer is implemented by processor types that record
// the timestamp of their first emitted output packet/frame
// (currently *processor.FromKernel via firstSeen.FirstOutputUnixNano).
// We type-assert via this interface here to avoid an import cycle:
// processor depends on protobuf for some converters, so this package
// can't import processor directly.
type firstFrameUnixNanoer interface {
	FirstFrameUnixNano() int64
}

func NodeToGRPC(
	ctx context.Context,
	n node.Abstract,
) *avpipelinegrpc.Node {
	if n == nil {
		return nil
	}
	proc := n.GetProcessor()
	result := &avpipelinegrpc.Node{
		Id:          uint64(n.GetObjectID()),
		Type:        fmt.Sprintf("%T", n),
		Description: n.String(),
		IsServing:   n.IsServing(ctx),
		Counters:    NodeCountersToGRPC(n.GetCountersPtr(), proc.CountersPtr()),
	}
	if ffter, ok := proc.(firstFrameUnixNanoer); ok {
		result.FirstFrameUnixNs = ffter.FirstFrameUnixNano()
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
		// Log instead of panicking — NextLayer may fail transiently
		// (e.g., concurrent node removal) and callers use this for
		// debug/monitoring serialization.
		result.Description += fmt.Sprintf(" [NextLayer error: %v]", err)
		return result
	}

	for _, nextNode := range nextLayer {
		result.ConsumingNodes = append(result.ConsumingNodes, NodeToGRPC(ctx, nextNode))
	}

	return result
}
