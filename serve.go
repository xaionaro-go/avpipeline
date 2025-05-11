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
	dstAlreadyVisited := map[node.Abstract]struct{}{}
	serve(ctx, serveConfig, errCh, &nodesWG, dstAlreadyVisited, nodes...)
}
func serve[T node.Abstract](
	ctx context.Context,
	serveConfig ServeConfig,
	errCh chan<- node.Error,
	nodesWG *sync.WaitGroup,
	dstAlreadyVisited map[node.Abstract]struct{},
	nodes ...T,
) {
	for _, n := range nodes {
		logger.Tracef(ctx, "Serve[%s]", n)
		func(n T) {
			if _, ok := dstAlreadyVisited[n]; ok {
				logger.Tracef(ctx, "/Serve[%s]: already visited", n)
				return
			}
			dstAlreadyVisited[n] = struct{}{}

			childrenCtx, childrenCancelFn := context.WithCancel(xcontext.DetachDone(ctx))
			for _, pushTo := range n.GetPushPacketsTos() {
				serve(childrenCtx, serveConfig, errCh, nodesWG, dstAlreadyVisited, pushTo.Node)
			}
			for _, pushTo := range n.GetPushFramesTos() {
				serve(childrenCtx, serveConfig, errCh, nodesWG, dstAlreadyVisited, pushTo.Node)
			}

			if serveConfig.NodeFilter != nil && !serveConfig.NodeFilter.Match(ctx, n) {
				logger.Tracef(ctx, "/Serve[%s]: skipped", n)
				return
			}
			nodesWG.Add(1)
			observability.Go(ctx, func() {
				defer nodesWG.Done()
				defer logger.Tracef(ctx, "/Serve[%s]: ended", n)
				defer func() {
					logger.Debugf(ctx, "cancelling context...")
					childrenCancelFn()
				}()
				logger.Tracef(ctx, "Serve[%s]: started", n)
				n.Serve(ctx, serveConfig.EachNode, errCh)
			})
		}(n)
	}
}
