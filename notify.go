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
	logger.Debugf(ctx, "NotifyAboutPacketSources: %T, %v", packetSource, nodes)
	defer func() {
		logger.Debugf(ctx, "NotifyAboutPacketSources: %T, %v: %v", packetSource, nodes, _err)
	}()

	if len(nodes) != 1 {
		return fmt.Errorf("we currently require exactly one node in the arguments; to be fixed in the future")
	}
	n := nodes[0]

	if packetSource != nil {
		if getPacketSinker, ok := n.GetProcessor().(interface{ GetPacketSink() packet.Sink }); ok {
			if packetSink := getPacketSinker.GetPacketSink(); packetSink != nil {
				if err := packetSink.NotifyAboutPacketSource(ctx, packetSource); err != nil {
					return fmt.Errorf("unable to notify '%s' (%p) with '%s' (%p): %w", packetSink, packetSink, packetSource, packetSource, err)
				}
			}
		}
	}
	if getPacketSourcer, ok := n.GetProcessor().(interface{ GetPacketSource() packet.Source }); ok {
		if newPacketSource := getPacketSourcer.GetPacketSource(); newPacketSource != nil {
			newPacketSource.WithOutputFormatContext(ctx, func(fc *astiav.FormatContext) {
				packetSource = newPacketSource
			})
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
