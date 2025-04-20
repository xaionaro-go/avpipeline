package avpipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

func NotifyAboutPacketSourcesRecursively[T AbstractNode](
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
	node := nodes[0]

	if getPacketSourcer, ok := node.GetProcessor().(interface{ GetPacketSource() packet.Source }); ok {
		if newPacketSource := getPacketSourcer.GetPacketSource(); newPacketSource != nil {
			hasNewFormatContext := false
			newPacketSource.WithFormatContext(ctx, func(fc *astiav.FormatContext) {
				hasNewFormatContext = true
			})
			logger.Debugf(ctx, "%T: hasNewFormatContext:%t", node, hasNewFormatContext)
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

	dstAlreadyProcessed := map[AbstractNode]struct{}{}
	for _, pushTo := range node.GetPushPacketsTos() {
		if _, ok := dstAlreadyProcessed[pushTo.Node]; ok {
			continue
		}
		dstAlreadyProcessed[pushTo.Node] = struct{}{}
		if err := NotifyAboutPacketSourcesRecursively(ctx, packetSource, pushTo.Node); err != nil {
			return fmt.Errorf("got error from %s: %w", pushTo.Node, err)
		}
	}
	for _, pushTo := range node.GetPushFramesTos() {
		if _, ok := dstAlreadyProcessed[pushTo.Node]; ok {
			continue
		}
		dstAlreadyProcessed[pushTo.Node] = struct{}{}
		if err := NotifyAboutPacketSourcesRecursively(ctx, packetSource, pushTo.Node); err != nil {
			return fmt.Errorf("for error from %s: %w", pushTo.Node, err)
		}
	}

	return nil
}

func ServeRecursively[T AbstractNode](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- ErrNode,
	nodes ...T,
) {
	var nodesWG sync.WaitGroup
	defer nodesWG.Wait()

	for _, node := range nodes {
		func(node T) {
			logger.Tracef(ctx, "ServeRecursively[%s]", node)
			defer func() { logger.Tracef(ctx, "/ServeRecursively[%s]", node) }()

			childrenCtx, childrenCancelFn := context.WithCancel(xcontext.DetachDone(ctx))
			dstAlreadyStarted := map[AbstractNode]struct{}{}
			for _, pushTo := range node.GetPushPacketsTos() {
				if _, ok := dstAlreadyStarted[pushTo.Node]; ok {
					continue
				}
				pushTo := pushTo
				nodesWG.Add(1)
				observability.Go(ctx, func() {
					defer nodesWG.Done()
					ServeRecursively(childrenCtx, serveConfig, errCh, pushTo.Node)
				})
				dstAlreadyStarted[pushTo.Node] = struct{}{}
			}
			for _, pushTo := range node.GetPushFramesTos() {
				if _, ok := dstAlreadyStarted[pushTo.Node]; ok {
					continue
				}
				pushTo := pushTo
				nodesWG.Add(1)
				observability.Go(ctx, func() {
					defer nodesWG.Done()
					ServeRecursively(childrenCtx, serveConfig, errCh, pushTo.Node)
				})
				dstAlreadyStarted[pushTo.Node] = struct{}{}
			}

			nodesWG.Add(1)
			observability.Go(ctx, func() {
				defer nodesWG.Done()
				defer func() {
					logger.Debugf(ctx, "cancelling context...")
					childrenCancelFn()
				}()
				node.Serve(ctx, serveConfig, errCh)
			})
		}(node)
	}
}
