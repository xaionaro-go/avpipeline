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
) error {
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
	if len(nodes) != 1 {
		errCh <- ErrNode{Err: fmt.Errorf("we currently require exactly one node in the arguments; to be fixed in the future")}
	}

	node := nodes[0]
	logger.Tracef(ctx, "ServeRecursively[%s]", node)
	defer func() { logger.Tracef(ctx, "/ServeRecursively[%s]", node) }()

	childrenCtx, childrenCancelFn := context.WithCancel(xcontext.DetachDone(ctx))
	var wg sync.WaitGroup
	dstAlreadyStarted := map[AbstractNode]struct{}{}
	for _, pushTo := range node.GetPushPacketsTos() {
		if _, ok := dstAlreadyStarted[pushTo.Node]; ok {
			continue
		}
		pushTo := pushTo
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			ServeRecursively(childrenCtx, serveConfig, errCh, pushTo.Node)
		})
		dstAlreadyStarted[pushTo.Node] = struct{}{}
	}
	for _, pushTo := range node.GetPushFramesTos() {
		if _, ok := dstAlreadyStarted[pushTo.Node]; ok {
			continue
		}
		pushTo := pushTo
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()
			ServeRecursively(childrenCtx, serveConfig, errCh, pushTo.Node)
		})
		dstAlreadyStarted[pushTo.Node] = struct{}{}
	}
	defer wg.Wait()
	defer childrenCancelFn()

	node.Serve(ctx, serveConfig, errCh)
}
