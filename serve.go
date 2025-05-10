package avpipeline

import (
	"context"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/node/condition"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xcontext"
)

type ServeConfig struct {
	EachNode   node.ServeConfig
	NodeFilter condition.Condition
}

func Serve[T node.Abstract](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- node.Error,
	nodes ...T,
) {
	var nodesWG sync.WaitGroup
	defer nodesWG.Wait()

	for _, n := range nodes {
		func(n T) {
			logger.Tracef(ctx, "ServeRecursively[%s]", n)
			defer func() { logger.Tracef(ctx, "/ServeRecursively[%s]", n) }()

			childrenCtx, childrenCancelFn := context.WithCancel(xcontext.DetachDone(ctx))
			dstAlreadyVisited := map[node.Abstract]struct{}{}
			for _, pushTo := range n.GetPushPacketsTos() {
				if _, ok := dstAlreadyVisited[pushTo.Node]; ok {
					continue
				}
				dstAlreadyVisited[pushTo.Node] = struct{}{}
				if serveConfig.NodeFilter != nil && !serveConfig.NodeFilter.Match(ctx, pushTo.Node) {
					continue
				}
				pushTo := pushTo
				nodesWG.Add(1)
				observability.Go(ctx, func() {
					defer nodesWG.Done()
					Serve(childrenCtx, serveConfig, errCh, pushTo.Node)
				})
			}
			for _, pushTo := range n.GetPushFramesTos() {
				if _, ok := dstAlreadyVisited[pushTo.Node]; ok {
					continue
				}
				dstAlreadyVisited[pushTo.Node] = struct{}{}
				if serveConfig.NodeFilter != nil && !serveConfig.NodeFilter.Match(ctx, pushTo.Node) {
					continue
				}
				pushTo := pushTo
				nodesWG.Add(1)
				observability.Go(ctx, func() {
					defer nodesWG.Done()
					Serve(childrenCtx, serveConfig, errCh, pushTo.Node)
				})
			}

			nodesWG.Add(1)
			observability.Go(ctx, func() {
				defer nodesWG.Done()
				defer func() {
					logger.Debugf(ctx, "cancelling context...")
					childrenCancelFn()
				}()
				n.Serve(ctx, serveConfig.EachNode, errCh)
			})
		}(n)
	}
}
