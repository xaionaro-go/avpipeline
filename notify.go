package avpipeline

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
)

func NotifyAboutPacketSources[T node.Abstract](
	ctx context.Context,
	packetSource packet.Source,
	nodes ...T,
) (_err error) {
	logger.Debugf(ctx, "NotifyAboutPacketSourcesRecursively: %T, %v", packetSource, nodes)
	defer func() {
		logger.Debugf(ctx, "NotifyAboutPacketSourcesRecursively: %T, %v: %v", packetSource, nodes, _err)
	}()

	if len(nodes) != 1 {
		return fmt.Errorf("we currently require exactly one node in the arguments; to be fixed in the future")
	}
	n := nodes[0]

	if getPacketSourcer, ok := n.GetProcessor().(interface{ GetPacketSource() packet.Source }); ok {
		if newPacketSource := getPacketSourcer.GetPacketSource(); newPacketSource != nil {
			hasNewFormatContext := false
			newPacketSource.WithFormatContext(ctx, func(fc *astiav.FormatContext) {
				hasNewFormatContext = true
			})
			logger.Debugf(ctx, "%T: hasNewFormatContext:%t", n, hasNewFormatContext)
			if hasNewFormatContext {
				if packetSource != nil {
					if err := newPacketSource.NotifyAboutPacketSource(ctx, packetSource); err != nil {
						return fmt.Errorf("unable to notify '%s' with '%s': %w", newPacketSource, packetSource, err)
					}
				}
				packetSource = newPacketSource
			}
		}
	}

	dstAlreadyProcessed := map[node.Abstract]struct{}{}
	for _, pushTo := range n.GetPushPacketsTos() {
		if _, ok := dstAlreadyProcessed[pushTo.Node]; ok {
			continue
		}
		dstAlreadyProcessed[pushTo.Node] = struct{}{}
		if err := NotifyAboutPacketSources(ctx, packetSource, pushTo.Node); err != nil {
			return fmt.Errorf("got error from %s: %w", pushTo.Node, err)
		}
	}
	for _, pushTo := range n.GetPushFramesTos() {
		if _, ok := dstAlreadyProcessed[pushTo.Node]; ok {
			continue
		}
		dstAlreadyProcessed[pushTo.Node] = struct{}{}
		if err := NotifyAboutPacketSources(ctx, packetSource, pushTo.Node); err != nil {
			return fmt.Errorf("for error from %s: %w", pushTo.Node, err)
		}
	}

	return nil
}
